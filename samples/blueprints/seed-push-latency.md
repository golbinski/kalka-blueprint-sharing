---
kalka_status: seed
kalka_origin: 3b1f6a52-8c47-4e0d-9a2b-5d7e41c0f8a6#9e41b7c20d5a4f83a6c1e07b2d94f358
kalka_created: 2026-11-10T14:22:05Z
---

# Show how long a push waited

## Problem

A reviewer who releases a version cannot tell whether it reached the team. The Team page says only "synced", and a push that waits behind a long round looks the same as one that went out at once. When a teammate says "I do not see version 3", the only evidence is the daemon log.

## Why it was split off

It came up in review of "A push during a round is not lost", as a deviation: the fix there is about not losing the push, and timing it is a separate feature with its own surface (the Team page and `kalka status`). Adding it would have doubled that plan's scope. Kept here until someone takes it up.
