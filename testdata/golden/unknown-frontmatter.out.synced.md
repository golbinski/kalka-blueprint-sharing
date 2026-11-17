---
# Written by an agent pipeline; kalka must not touch these lines.
kalka_id: 5a2c1b6d-7e9f-4b8a-9d0c-4f6a7b8c9d0e
kalka_owner: alice@corp.com
kalka_team: parcels
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

### release · alice@corp.com · 2026-11-12T11:00:00Z · § · rev:766849ce · id:cfd5247390d2d576f668640e08e94f85 · sig:a2fe011ecf3e953a.k0aH_U2WuYjQlMvAalG_7GWVM2015B-iFz3RiYbKtt2lNY0fsutlaapQ_xMVzybwUEkm7Zv9wk1lApxpfx_-Bg
v1 · plan:a679891918eb96ca16c0eb65d6af2a16b3d5fb3662be0e93cb882f403f07da26 · owner:alice@corp.com

### comment · bob@example.org · 2026-11-12T12:00:00Z · §plan · rev:766849ce · id:f5052c934c7a032f91d121ec433db641 · sig:64d819232537a1c4.p_kIlX4ljPgGe34rvDpOkqpBdZums2cB9XVOr4nz-kyEM5KKnYviB3GpDschXBRqMJoKeqohWY_qflNai4T7Cg
Golden comment.
