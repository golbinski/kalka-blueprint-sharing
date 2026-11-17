---
# Written by an agent pipeline; kalka must not touch these lines.
kalka_id: 5a2c1b6d-7e9f-4b8a-9d0c-4f6a7b8c9d0e
kalka_owner: alice@corp.com
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
