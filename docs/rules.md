# kalka rules

<!-- Generated from internal/rules; run `go test ./internal/rules -update`. -->

Every diagnostic of `kalka check` and of the app has one of these ids (specification 6.17). `kalka check --explain <id>` prints what a rule means, why it exists, and a wrong and a right example.

## Frontmatter (F)

| Rule | Severity | What it means |
|---|---|---|
| `F001` | error | **Frontmatter never closed.** The file opens frontmatter with `---` and never closes it, so all of it is read as body. |
| `F002` | error | **kalka_id is not a UUID.** `kalka_id:` is not a version 4 UUID. |
| `F003` | error | **Shared blueprint has no owner.** The document has a `kalka_id:` but no `kalka_owner:`. |
| `F004` | error | **Owner is not an email address.** `kalka_owner:` is not an email address. |
| `F005` | info | **Local blueprint.** The document has no `kalka_id:` yet: it is local and gets one on first publish. |
| `F010` | warning | **Domain name missing or malformed.** A domain document (`kalka_kind: domain`) has no valid `kalka_domain:` name: lower-case letters, digits and dashes. |
| `F011` | warning | **Domain version is not a number.** `kalka_version:` of a domain document is not a positive number. |

## Structure and the review log (B)

| Rule | Severity | What it means |
|---|---|---|
| `B000` | error | **File cannot be read.** The file cannot be read, is not UTF-8 text kalka can parse, or is over the limits (2 MiB). |
| `B001` | error | **Body ends inside an open block.** The body ends inside an unclosed code fence or HTML block. |
| `B002` | warning | **Second review log heading.** A second `## Review log` heading is ordinary body text; only the first is the review log. |
| `B003` | warning | **Unparsed text in the review log.** Text in the review log is not an entry; it is kept as it is but takes no part in review. |
| `B010` | warning | **Edited entry.** An entry's id no longer matches its content: it was edited after it was written. |
| `B011` | warning | **Entry appears twice.** The same entry appears more than once in the log. |
| `B012` | error | **Entry too large.** An entry's body is larger than 64 KiB. |
| `B013` | error | **Entry opens a fence it never closes.** An entry body opens a code fence and never closes it. |
| `B014` | warning | **Heading inside an entry.** Headings inside an entry must be level 4 or deeper. |
| `B015` | warning | **Comment on a removed section.** An entry is anchored to a section, trace step or invariant that is no longer in the body. |
| `B016` | info | **Comment on an earlier version.** An entry was written against an earlier text of the body. |

## Trace blocks (T)

| Rule | Severity | What it means |
|---|---|---|
| `T001` | error | **Trace YAML does not parse.** The content of the trace block is not well-formed YAML. |
| `T002` | error | **YAML feature not allowed.** Anchors, aliases, explicit tags, merge keys or more than one YAML document. |
| `T003` | error | **Key twice in one mapping.** A key appears twice in one mapping. |
| `T004` | error | **Block is not a mapping.** The trace block is not a mapping with lanes and nodes. |
| `T005` | error | **Unknown trace version.** The fence names a version this kalka does not know, or its second word is not a version. |
| `T010` | error | **No lanes.** `lanes` is missing or is not a list of texts. |
| `T011` | error | **Wrong number of lanes.** Fewer than 2 or more than 5 lanes. |
| `T012` | error | **Bad lane name.** A lane name is empty, longer than 40 characters, or listed twice. |
| `T020` | error | **No nodes.** `nodes` is missing, is not a list, or is empty. |
| `T021` | error | **Node is not a mapping.** An entry of `nodes` is not a block mapping. |
| `T022` | error | **Too many nodes.** More than 40 nodes. |
| `T030` | error | **Bad node id.** `id` is missing or is not a letter followed by letters, digits, - and _ (at most 32). |
| `T031` | error | **Node id used twice.** Two nodes have the same id. |
| `T032` | error | **Unknown lane.** `lane` is missing or is not one of `lanes`. |
| `T033` | error | **Node has no text.** `text` is missing or empty. |
| `T034` | error | **Value has the wrong shape.** A value is not of its key's form: a text where a list is expected, a list where a text is, or the retired `{to: …, label: …}` edge. |
| `T040` | error | **Unknown node named.** `next`, `back` or `realizes` names an id that is not a node of the block. |
| `T041` | error | **Edge across lanes.** `next` or `back` names a node in another lane. |
| `T042` | error | **Node follows itself.** `next` names the node itself. |
| `T043` | error | **Cycle in next.** The `next` edges form a cycle. |
| `T044` | error | **back goes forward.** `back` names a node that is neither the node itself nor one before it. |
| `T045` | error | **realizes names a lower lane.** `realizes` names a node that is not in a lane above. |
| `T046` | error | **Unknown change.** `change` is not `new`, `modified` or `removed`. |
| `T101` | warning | **Trace fence without version.** The fence has no version; the block is read as v1. |
| `T102` | warning | **Unknown key.** A key that is not part of the language and does not start with `x-`; it is ignored. |
| `T103` | warning | **Comment after a value.** A YAML comment follows a value on the same line. |
| `T104` | warning | **Id named twice.** An id is named twice in one `next`, `back` or `realizes`, or under both `next` and `back`. |
| `T105` | warning | **Redundant realizes.** A node realizes a node directly and also through another node it realizes. |
| `T106` | warning | **Empty lane.** A lane has no nodes. |
| `T107` | warning | **Lane in pieces.** The nodes of a lane fall into groups that no `next` or `back` edge joins. |
| `T108` | warning | **Many nodes.** More than 20 nodes. |
| `T109` | warning | **Text too long.** `text` longer than 100 characters, `why` longer than 300, or a label longer than 40. |
| `T110` | warning | **Step without why.** A live node outside the first lane realizes nothing and has no `why`. |
| `T111` | warning | **Ids differ only in case.** Two ids differ only in letter case. |
| `T112` | warning | **ref is not scheme:payload.** `ref` is not of the form `scheme:payload`, or contains white space. |
| `T113` | warning | **Some branches unlabelled.** A node has several `next` edges and only some have a label. |
| `T120` | warning | **Node id shared under one heading.** Two trace blocks under the same heading both have a node with this id. |
| `T121` | warning | **Trace without a heading.** The block has no heading above it, so its steps cannot be commented on. |
| `T122` | warning | **YAML block that looks like a trace.** A block fenced as `yaml` has a root mapping with both `lanes` and `nodes`. |

## References to other documents (R)

| Rule | Severity | What it means |
|---|---|---|
| `R001` | info | **Domain version not here, can't check.** The domain version `kalka_domain:` names is not available, so the glossary and invariant rules that need it were not run. |
| `R002` | warning | **Malformed domain reference.** `kalka_domain:` in a blueprint is not `<team>/<domain>@v<N>`. |
| `R003` | info | **Newer domain version.** A newer version of the referenced domain is available. |
| `R010` | warning | **Reference does not resolve.** With --with-plugins: a trace `ref` or an invariant's `verified_by` reference has no plugin or does not resolve. |
| `R011` | warning | **Referenced lines changed.** With --with-plugins: a plugin warns about a reference, for example that its lines changed since the plan was written. |

## Hints (H)

| Rule | Severity | What it means |
|---|---|---|
| `H001` | info | **No domain reference.** An opted-in blueprint names no domain version: no `kalka_domain:` and no "Domain reference" section. |
| `H002` | info | **No scope.** An opted-in blueprint has no "Scope" section. |
| `H003` | info | **No verification plan.** An opted-in blueprint has neither an invariants block nor a "Verification plan" section. |
| `H004` | info | **No critical sections.** An opted-in blueprint has no "Critical sections" section. |
| `H005` | info | **No operations.** An opted-in blueprint has no "Operations" section. |
| `H010` | info | **Unknown blueprint reference version.** The document follows a version of the blueprint reference this kalka does not know; no hints are given. |

