---
# Written by an agent pipeline; kalka must not touch these lines.
kalka_id: 5a2c1b6d-7e9f-4b8a-9d0c-4f6a7b8c9d0e
owner: alice@corp.com
team: parcels
repo: org/doc-service        # read by the gh plugin
base_sha: 3f9c2a1
pipeline:
  stage: grooming
  steps: [draft, review, accept]
  reviewers:
    - bob@corp.com
    - {email: carol@corp.com, role: security}
summary: >
  A folded multi-line string
  that spans lines.
notes: |
  ## Review log
  this is not a heading, it is YAML
empty:
"quoted key": 'single: quoted'
---

# Import pipeline refactor

## Plan
See the parser.

## Review log

### release · alice@corp.com · 2026-11-12T11:00:00Z · § · rev:766849ce · id:cfa87c6f80aa · sig:fZ8AdagSU2Zg8XMwoiIBpocTKtNSGSmho0lFyDaWY039pgjX4Vn4D5XR9sW2q2BKCLf5U3yYcQ5i4mhsQZPRDQ
v1 · plan:70ad50d1aa2871d0b026bc22dae7bb0e5dec756e13d910173a57d489a89d7259 · owner:alice@corp.com

### comment · bob@example.org · 2026-11-12T12:00:00Z · §plan · rev:766849ce · id:0658c20623ab · sig:Dia4YOC06HSCoh3WGTBfYpd19GgYeaz0iBPbvfe4ayvX9MBulPtZ0UTd3r2yf5ztNvbe2u68pMHSSmvGEj9gCg
Golden comment.
