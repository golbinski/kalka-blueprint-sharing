---
kalka_kind: domain
kalka_domain: release
kalka_version: 1
repo: golbinski/kalka-blueprint-sharing
base_sha: 54b089f5733189be0b75fb1704efc8236109de74
---

# Release packaging

The language of how kalka is built, packaged and published, and what must hold for every release. The packaging team's plans name the version they were written against with `kalka_domain: packaging/release@v<N>`. The file says `kalka_version: 1`, as a released copy does, so `kalka check --domain` can read it from a repository.

## Glossary

```glossary v2
term release: The set of artifacts published for one tag, with the checksum file that covers them
  form releases
  see tag
  see pre-release
  context publish

term tag: A git tag of the form v1.2.3, or v1.2.3-rc1 with a suffix, that starts a release
  form tags
  context publish

term pre-release: A release whose tag has a hyphen; it is published but not offered as the latest
  form pre-releases
  alias release candidate
  alias release candidates
  context publish

term artifact: One file a release publishes: an archive, an installer or the checksum file
  form artifacts
  alias build output
  alias build outputs
  context build

term archive: A zip or tar.gz holding the binaries and a readme for one platform and architecture
  form archives
  context build

term installer: A per-user Windows setup executable built with NSIS, for one architecture
  form installers
  alias setup program
  context build
```

## Parts

- **Build**: compiles the programs for each platform and stages them. Knows nothing about GitHub.
- **Publish**: turns a tag into a release on GitHub, from the artifacts the build left in `dist`.

## Invariants

```invariants v2
invariant I1: The checksum file lists every artifact of a release, and only those
  why a user verifies a download against it; a missing line reads as a bad download
  verified-by gh:tools/release/release_test.go#L217-L277

invariant I2: kalka.exe is a console program and kalka-desktop.exe a GUI one
  why a mix-up opens a console at every logon, or makes the command line print nothing
  verified-by gh:tools/release/release_test.go#L86-L127
  verified-by gh:tools/release/pe.go#L33-L49

invariant I3: A tag with a hyphen is published as a pre-release, and a tag without one is not
  verified-by gh:.github/workflows/ci.yml#L137-L141
  verified-by Reviewer: push a tag ending in -rc1 to a fork and read the release page

invariant I4: A release never publishes a desktop executable without the app icon
  why a user sees the default icon on the taskbar and takes the app for a leftover
  verified-by gh:tools/release/pe.go#L50-L58
```
