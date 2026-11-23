# Security

## Reporting a vulnerability

Please report vulnerabilities privately, through **Report a vulnerability** on the repository's *Security* tab (GitHub's private vulnerability reporting), not in a public issue. Say what is affected, the version (`kalka version`), and how to reproduce it. You will get an answer within a week; a fix is released as a patch version and the advisory published with it, crediting you unless you ask otherwise.

Only the latest release is supported: kalka is before 1.0, and fixes go into the next release, which the desktop app installs itself (spec 24).

## What a release proves

Every release is built by the project's GitHub Actions workflow (`.github/workflows/ci.yml`) from the tagged commit, after the tests pass on Linux, macOS and Windows. Nobody uploads a file by hand. Each release carries:

- **`SHA256SUMS`**, the checksum of every file in the release.
- **`SHA256SUMS.minisig`**, a [minisign](https://jedisct1.github.io/minisign/) signature over `SHA256SUMS` with the kalka release key. Its trusted comment names the release (`kalka v0.3.1 SHA256SUMS`), so a signature cannot be moved to another release.
- **A build provenance attestation** for every file ([SLSA](https://slsa.dev) provenance, signed through GitHub's Sigstore instance): which repository, workflow, commit and run built it.

The desktop app's updater installs nothing whose checksum does not match a `SHA256SUMS` signed with a key built into the running kalka, for that release (spec 24.2).

Code signing of the programs themselves (Authenticode on Windows, Developer ID and notarization on macOS) is not in place yet; see spec 23.5 and `docs/install.md` for what that means when you first start kalka.

## Checking a download

The release key:

```
RWR1mOYbi9qbgJl371Oqgkl243JEuiMNnlOKS0dntAZynje7S5Espx/f
```

With [minisign](https://jedisct1.github.io/minisign/), in the folder holding the download, `SHA256SUMS` and `SHA256SUMS.minisig`:

```sh
minisign -Vm SHA256SUMS -P RWR1mOYbi9qbgJl371Oqgkl243JEuiMNnlOKS0dntAZynje7S5Espx/f
sha256sum --ignore-missing -c SHA256SUMS     # macOS: shasum -a 256 --ignore-missing -c SHA256SUMS
```

The first line must print `Signature and comment signature verified` and the trusted comment with the release's version; the second, `OK` for your file.

With the [GitHub CLI](https://cli.github.com), the provenance of a file:

```sh
gh attestation verify kalka_0.3.1_linux_amd64.tar.gz --repo golbinski/kalka-blueprint-sharing
```

It checks that the file was built by this repository's workflow, and prints the commit.

## The release key

The key's secret is held only as a secret of the repository's `release` environment, which only `v*` tags may use, and offline by the maintainer. A new key is announced here and built into kalka (`internal/update/minisign.go`) one release before releases are signed with it, so the updater of every supported version keeps verifying.

## Dependencies

`govulncheck` runs on every push and fails the build on a known vulnerability in code kalka calls, the Go standard library's included. The GitHub Actions the workflows use are pinned to commits, and Dependabot proposes updates to them and to the Go and npm dependencies.
