---
kalka_reference: kalka-blueprint@v2
kalka_domain: packaging/release@v1
repo: golbinski/kalka-blueprint-sharing
base_sha: 8d56d5b47dbb7bfe0316f8b128fa1b8ed658c160
repos:
  wails: {repo: wailsapp/wails, base_sha: 4b454e443f6f11ccb64679c5c3d3f3bf5e6fb440}
---

# The app icon on the Windows taskbar

A second blueprint for `packaging`, on the same domain as the installer plan, spanning two repositories: kalka, the default, and the Wails desktop runtime it builds on, named `wails` in the frontmatter.

## Context

On Windows, notifications showed kalka's icon, but the taskbar button, the Start menu shortcut and *Installed apps* showed the default application icon. kalka hands Wails the icon as image data ([Icon](gh:internal/desktop/wails.go#L153)); Wails uses that only for the window's own large icon, after looking for an icon resource in the executable first ([window icon](gh:wails:v3/pkg/application/webview_window_windows.go#L660-L674)). The window class asks the executable for `IDI_APPLICATION` ([window class](gh:wails:v3/pkg/application/application_windows.go#L212-L226)), and Explorer, the shortcut and the taskbar read icon index 0 of the executable. `kalka-desktop.exe` had no icon resources at all.

## Scope

A new package that writes Windows icon resources, the generator for `cmd/kalka-desktop`, and a release check. Not the icon's design, not the Wails runtime.

## Plan

1. [winres](gh:internal/winres/winres.go#L51-L207) writes a COFF object with the icon at eight sizes, as one icon group stored under id 3 and under `IDI_APPLICATION`.
2. [winicon](gh:tools/winicon/main.go#L19-L58) draws the sizes from the app icon and writes `rsrc_windows_amd64.syso` and `rsrc_windows_arm64.syso`, which the Go linker embeds in every Windows build of the package. They are committed, so a plain `go build` gets them.
3. The release refuses a `kalka-desktop.exe` without the icon ([check](gh:tools/release/pe.go#L50-L58)).

```trace v2
lane business

step B1: The taskbar and Start menu show kalka's icon
  next B2
step B2: Notifications keep showing it

lane code

step C1: Icon group id 3 in the executable
  next C2
  realizes B1
  realizes B2
  ref gh:wails:v3/pkg/application/webview_window_windows.go#L660-L674
  guards A1
  why Wails loads id 3 for the window and for the notification image
step C2: The same group as IDI_APPLICATION
  next C3
  ref gh:wails:v3/pkg/application/application_windows.go#L212-L226
  why the window class asks the executable for it
step C3: Refuse a release without the icon
  ref gh:tools/release/pe.go#L50-L58
  guards release#I4
  why a build that lost the resources would ship with the default icon unnoticed
```

## Verification plan

```invariants v2
affects release#I4

invariant A1: The icon group with the lowest id in kalka-desktop.exe is the app icon
  why Explorer takes icon index 0 to be the lowest id
  verified-by gh:internal/winres/winres_test.go#L14-L62
  status new
```

- [TestLinkedIcon](gh:internal/winres/winres_test.go#L14-L62) links the object into Windows programs for amd64 and arm64 and reads both groups back.
- The committed objects match what the generator draws now, within rounding between architectures.
- On a Windows machine, after installing over the previous version: the taskbar button, the Start menu entry and *Installed apps* show the icon. A cached old icon goes after unpinning, or after signing out once.

## Critical sections

Explorer takes icon index 0 to be the group with the lowest id. Group 3 must stay the lowest, or shortcuts would show whichever group comes first. Wails' notification helper also loads id 3 ([saveIconToDir](gh:wails:v3/pkg/services/notifications/notifications_windows.go#L612-L619)), so 3 is not a free choice.

## Operations

- Failure: a release without the icon fails in the release job, before anything is published.
- Rollback: delete the two `.syso` files; the app goes back to the default icon.
