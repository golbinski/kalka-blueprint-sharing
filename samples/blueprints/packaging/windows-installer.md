---
reference: kalka-blueprint@v1
repo: golbinski/kalka-blueprint-sharing
base_sha: 618f7edcaba5b7419069a76c039151fd26366718
---

# The release builds the Windows installer

A blueprint for a second team (`packaging`), to try copying and moving documents between teams.

## Context

The release job runs on Linux. The release tool builds the Windows installer only when `makensis` is on the PATH ([installer](gh:tools/release/desktop.go#L20-L50)), and the runner had none, so a tagged release had zip archives for Windows but no installer.

## Scope

The `release` job in [ci.yml](gh:.github/workflows/ci.yml#L114-L143) and the [NSIS script](gh:tools/release/kalka.nsi.tmpl). Not the release tool's build of the binaries, and not signing.

## Plan

1. Install NSIS in the release job, with the GTK 4 and WebKitGTK packages so the Linux desktop archive is built too.
2. Publish a tag with a hyphen, such as `v0.1.0-rc1`, as a pre-release.
3. Keep Kalka.app out of this job: it [needs a Mac](gh:tools/release/main.go#L265-L285).

## Verification plan

- A local run of the release tool for `windows/amd64` with version `0.1.0-rc1` writes `kalka_0.1.0-rc1_windows_amd64_setup.exe`.
- The tagged run's log has a "wrote" line for both Windows installers and the Linux amd64 desktop archive, and a "skip" line only for Kalka.app and the Linux arm64 desktop archive.
- The installer installs per user on a clean Windows machine and starts the app.

## Operations

- Failure: `apt-get` cannot reach its mirror and the job fails before building; re-run it. A failed release job publishes nothing.
- Rollback: delete the pre-release and its tag; no other job depends on them.
