---
kalka_reference: kalka-blueprint@v1
kalka_domain: packaging/release@v1
kalka_status: draft
repo: golbinski/kalka-blueprint-sharing
base_sha: 54b089f5733189be0b75fb1704efc8236109de74
ticket: PKG-12
labels: [windows, ci]
---

# The release builds the Windows installer

A blueprint for a second team, `packaging`, to try copying and moving documents between teams. It says `kalka_status: draft`, a label of the team's own, and carries two keys, `ticket` and `labels`, that kalka passes through untouched.

## Context

The release job runs on Linux. The release tool builds the Windows installer only when `makensis` is on the PATH ([installer](gh:tools/release/desktop.go#L21-L51)), and the runner had none, so a tagged release had zip archives for Windows but no installer.

## Scope

The `release` job in [ci.yml](gh:.github/workflows/ci.yml#L114-L143) and the [NSIS script](gh:tools/release/kalka.nsi.tmpl). Not the release tool's build of the binaries, and not signing.

## Plan

1. Install NSIS in the release job, with the GTK 4 and WebKitGTK packages so the Linux desktop archive is built too.
2. Publish a tag with a hyphen, such as `v0.1.0-rc1`, as a pre-release.
3. Keep Kalka.app out of this job: it [needs a Mac](gh:tools/release/main.go#L267-L286).

```trace v1
lanes: [business, code]
nodes:
  - id: B1
    lane: business
    text: "A tagged release ships a Windows installer"
    next: [B2]
  - id: B2
    lane: business
    text: "A pre-release is clearly marked as one"

  - id: C1
    lane: code
    text: "Install NSIS on the runner"
    realizes: [B1]
    change: new
    ref: "gh:.github/workflows/ci.yml#L130-L135"
    next: [C2]
  - id: C2
    lane: code
    text: "Build and sign the installer when makensis is found"
    realizes: [B1]
    guards: [W1, "release#I1"]
    ref: "gh:tools/release/desktop.go#L21-L51"
    next: [C3]
  - id: C3
    lane: code
    text: "A hyphen in the tag adds --prerelease"
    realizes: [B2]
    guards: ["release#I3"]
    ref: "gh:.github/workflows/ci.yml#L137-L141"
```

## Verification plan

```invariants v1
affects: ["release#I1", "release#I3"]
invariants:
  - id: W1
    statement: "A tagged run publishes an installer for each Windows architecture, or fails"
    verified_by: ["Reviewer: the tagged run's log has a wrote line for both installers"]
    status: new
```

- A local run of the release tool for `windows/amd64` with version `0.1.0-rc1` writes `kalka_0.1.0-rc1_windows_amd64_setup.exe`.
- The tagged run's log has a "wrote" line for both Windows installers and the Linux amd64 desktop archive, and a "skip" line only for Kalka.app and the Linux arm64 desktop archive.
- The installer installs per user on a clean Windows machine and starts the app.

## Operations

- Failure: `apt-get` cannot reach its mirror and the job fails before building; re-run it. A failed release job publishes nothing.
- Rollback: delete the pre-release and its tag; no other job depends on them.
