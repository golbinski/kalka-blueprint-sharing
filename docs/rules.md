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
| `F020` | warning | **Issue link malformed.** `kalka_issue:` is not `<scheme>:<issue>`: a plugin scheme, a colon and the issue's key, with no spaces. |
| `F021` | info | **Issue scheme without a shipped plugin.** `kalka_issue:` names a scheme no plugin shipped with kalka handles; a plugin installed for it may. |
| `F022` | warning | **Archive time malformed.** `kalka_archived:` is not a UTC time; the blueprint is not treated as archived. |

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

## Glossary (G)

| Rule | Severity | What it means |
|---|---|---|
| `G001` | error | **Glossary YAML does not parse.** The content of a glossary block is not valid YAML. |
| `G002` | error | **Glossary uses a YAML feature that is not allowed.** The block uses anchors, aliases, tags, merge keys or several YAML documents. |
| `G003` | error | **Glossary key written twice.** One entry has the same key twice. |
| `G004` | error | **Glossary entry has the wrong shape.** The block is not a list of entries, an entry is not a block mapping, or a value is not a text or a list of texts. |
| `G005` | error | **Glossary version not known.** The fence names a version of the glossary language this kalka does not read. |
| `G006` | warning | **Glossary fence has no version.** The fence says `glossary` without a version; it is read as v1. |
| `G007` | warning | **Unknown glossary key.** An entry has a key that is not term, definition, aliases, forms, context or see, and does not start with `x-`. |
| `G008` | warning | **Glossary in a blueprint that names a domain.** A blueprint with `kalka_domain:` has a glossary block of its own; it is shown as code and links nothing. A blueprint that names no domain may keep its own glossary. |
| `G010` | error | **Glossary entry has no term.** An entry has no `term:`. |
| `G011` | error | **Term has no definition.** An entry has a term but no `definition:`. |
| `G012` | error | **A word names two glossary entries.** A term, form or alias is the same (ignoring case) as a term, form or alias of another entry. |
| `G013` | error | **Term too long.** A term is longer than 60 characters. |
| `G014` | warning | **Too many terms.** The glossary has more than 500 terms; the rest are not read. |
| `G015` | warning | **Definition too long.** A definition is longer than 400 characters. |
| `G016` | warning | **see names an unknown term.** `see:` names a word that is not a term of the same glossary. |
| `G020` | warning | **Alias used instead of the term.** The text uses an alias of a glossary term; the diagnostic offers the term as a replacement. |
| `G030` | info | **Release would make held blueprints use an alias.** Shown to a domain owner releasing a new version: a held blueprint uses, as a word, an alias the new version introduces. |

## Invariants (I)

| Rule | Severity | What it means |
|---|---|---|
| `I001` | error | **Invariants YAML does not parse.** The content of an invariants block is not valid YAML. |
| `I002` | error | **Invariants use a YAML feature that is not allowed.** The block uses anchors, aliases, tags, merge keys or several YAML documents. |
| `I003` | error | **Invariants key written twice.** One mapping has the same key twice. |
| `I004` | error | **Invariants block has the wrong shape.** The block is not a mapping with an `invariants:` list, an invariant is not a block mapping, a value has the wrong type, or `affects` names something that is not `<domain>#<id>`. |
| `I005` | error | **Invariants version not known.** The fence names a version of the invariants language this kalka does not read. |
| `I006` | warning | **Invariants fence has no version.** The fence says `invariants` without a version; it is read as v1. |
| `I007` | warning | **Unknown invariants key.** A key is not one of the language's and does not start with `x-`. |
| `I008` | warning | **Blueprint-only key in a domain document.** A domain document uses `affects` or `status`, which only a blueprint has; they are ignored. |
| `I010` | error | **Invariant id is not valid.** An id is missing, does not start with a letter, uses characters other than letters, digits, `-` and `_`, or is longer than 32 characters. |
| `I011` | error | **Invariant id used twice.** Two invariants in one document have the same id. |
| `I012` | error | **Invariant has no statement.** An invariant has an id but no `statement:`. |
| `I013` | error | **Invariant status not known.** `status:` is not `new`, `changed` or `removed`. |
| `I014` | warning | **Too many invariants.** The document has more than 100 invariants; the rest are not read. |
| `I015` | warning | **Statement too long.** A statement is longer than 300 characters. |
| `I016` | warning | **Invariant id is also a trace step id.** An invariant and a trace step under the same heading share an id, so the anchor `§slug#id` names both. |
| `I020` | error | **Invariant reference does not resolve.** A `guards` or `affects` entry names an invariant that does not exist: an unknown id, a domain other than the one `kalka_domain:` names, or a domain reference with no `kalka_domain:` at all. |
| `I030` | warning | **No trace step guards the invariant.** The document has a trace, and no step lists the invariant (one it introduces or affects) under `guards`. |
| `I031` | warning | **Invariant not verified.** An invariant has no `verified_by`. |
| `I040` | info | **Referenced invariant removed in a newer domain version.** A blueprint references a domain invariant that a newer version of the domain no longer has. Also shown to the domain owner releasing that version, listing the held blueprints concerned. |

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

