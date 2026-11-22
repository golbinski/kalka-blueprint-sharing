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
| `F010` | warning | **Domain name missing or malformed.** A domain document (`kalka_kind: domain`) has no valid `kalka_domain:` name: segments of lower-case letters, digits and dashes, a subdomain's joined by dots. |
| `F011` | warning | **Domain version is not a number.** `kalka_version:` of a domain document is not a positive number. |
| `F012` | error | **Domain named parent.** A domain document is named `parent`, the reserved name of the parent plan node in references. |
| `F013` | warning | **Domain parent malformed.** A domain document's `kalka_parent:` is not `<team>/<domain>@v<N>`; it is ignored. |
| `F014` | warning | **Parent plan node malformed.** `kalka_parent:` of a document that is not a domain document is not a `kalka_id`; it is ignored. |
| `F015` | warning | **Seed origin malformed.** `kalka_origin:` is not `<kalka_id>` or `<kalka_id>#<entry id>`; it is ignored. |
| `F016` | warning | **Breakdown not known.** `kalka_breakdown:` is not `decomposed` or `adopted`; it is ignored. |
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

## The kalka language v2 (S)

| Rule | Severity | What it means |
|---|---|---|
| `S001` | error | **Tab in a block.** A line of a typed block holds a tab. The fix replaces each tab with two spaces. |
| `S002` | error | **Keyword not in lower case.** A keyword is written with capitals. The fix writes it in lower case. |
| `S003` | error | **Colon after a keyword.** A keyword is followed by a colon, as in YAML. The fix removes the colon. |
| `S004` | error | **Wrong indentation.** A line is indented by something other than nothing (a header) or exactly two spaces (an attribute). There is no third level. |
| `S005` | error | **Header line incomplete.** A header is not `<keyword> <name>: <text>` (an item) or `<keyword> <text>` (a section): the name, the colon or the text is missing. |
| `S006` | error | **Attribute with no item.** An indented attribute line comes before any item it could belong to. |
| `S007` | error | **Empty value.** A keyword has no value after it, or a label after `: ` is empty. |
| `S008` | error | **Keyword on the wrong level.** An attribute keyword starts a line in column 0, or a header keyword is indented. |
| `S009` | error | **Reference with spaces.** A keyword of type reference has a value with spaces, or several references on one line. |
| `S010` | error | **Familiar format instead of v2.** The block drifts into a familiar format: Gherkin (`Given`, `Scenario:`, `And`) or the retired YAML form (`- id:`, `lanes:`). The message, and where it can a fix, shows the v2 form. |
| `S011` | error | **Comment in a block.** A line of a typed block starts with `#` or `//`. |
| `S012` | error | **Block version not read.** The fence names a version this kalka does not read: v1, the retired YAML form, or a newer one. The block is shown as code. |
| `S013` | warning | **Fence without a version.** The fence names the block (`goals`) with no version; it is read as v2. |
| `S020` | warning | **Unknown keyword.** A line starts with a word that is not a keyword of the block and does not start with `x-`; the line, and for a header its attributes, is ignored. The message names the nearest keyword. |
| `S021` | error | **Keyword written twice.** A keyword that takes one value (`why`, `ref`, `status`, `change`, `context`) appears twice under one item; the second is ignored. |
| `S022` | error | **Value not one of the allowed words.** `status` is not `new`, `changed` or `removed`, or `change` is not `new`, `modified` or `removed`. |
| `S030` | error | **Not an id.** The name of an invariant, goal, criterion or step is not an id: a letter, then letters, digits, `-` and `_`, at most 32 characters. |
| `S031` | error | **Empty block.** A typed block has no items. |

## Trace blocks (T)

| Rule | Severity | What it means |
|---|---|---|
| `T011` | error | **Wrong number of lanes.** Fewer than 2 or more than 5 lanes. |
| `T012` | error | **Bad lane name.** A lane name is longer than 40 characters, or a lane is opened twice. |
| `T020` | error | **No steps.** The block has lanes and no steps. |
| `T022` | error | **Too many steps.** More than 40 steps. |
| `T031` | error | **Step id used twice.** Two steps have the same id. |
| `T032` | error | **Step before any lane.** A step comes before the first `lane` line. |
| `T040` | error | **Unknown step named.** `next`, `back` or `realizes` names an id that is not a step of the block. |
| `T041` | error | **Edge across lanes.** `next` or `back` names a step in another lane. |
| `T042` | error | **Step follows itself.** `next` names the step itself. |
| `T043` | error | **Cycle in next.** The `next` edges form a cycle. |
| `T044` | error | **back goes forward.** `back` names a step that is neither the step itself nor one before it. |
| `T045` | error | **realizes names a lower lane.** `realizes` names a step that is not in a lane above. |
| `T104` | warning | **Id named twice.** An id is named twice by one keyword of a step, or as both `next` and `back`. |
| `T105` | warning | **Redundant realizes.** A step realizes a step directly and also through another step it realizes. |
| `T106` | warning | **Empty lane.** A lane has no steps. |
| `T107` | warning | **Lane in pieces.** The steps of a lane fall into groups that no `next` or `back` edge joins. |
| `T108` | warning | **Many steps.** More than 20 steps. |
| `T109` | warning | **Text too long.** A step's text longer than 100 characters, `why` longer than 300, or a label longer than 40. |
| `T110` | warning | **Step without why.** A live step outside the first lane realizes nothing and has no `why`. |
| `T111` | warning | **Ids differ only in case.** Two ids differ only in letter case. |
| `T112` | warning | **ref is not scheme:payload.** `ref` is not of the form `scheme:payload`. |
| `T113` | warning | **Some branches unlabelled.** A step has several `next` edges and only some have a label. |
| `T120` | warning | **Step id shared under one heading.** Two trace blocks under the same heading both have a step with this id. |
| `T121` | warning | **Trace without a heading.** The block has no heading above it, so its steps cannot be commented on. |

## Glossary (G)

| Rule | Severity | What it means |
|---|---|---|
| `G008` | warning | **Glossary in a blueprint that names a domain.** A blueprint with `kalka_domain:` has a glossary block of its own; it is shown as code and links nothing. A blueprint that names no domain may keep its own glossary. |
| `G012` | error | **A word names two glossary entries.** A term, form or alias is the same (ignoring case) as a term, form or alias of another entry of the document. |
| `G013` | error | **Term too long.** A term is longer than 60 characters. |
| `G014` | warning | **Too many terms.** The glossary has more than 500 terms; the rest are not read. |
| `G015` | warning | **Definition too long.** A definition is longer than 400 characters. |
| `G016` | warning | **see names an unknown term.** `see` names a word that is not a term of the same glossary, or of a domain above it. |
| `G020` | warning | **Alias used instead of the term.** The text uses an alias of a glossary term; the diagnostic offers the term as a replacement. |
| `G030` | info | **Release would make held blueprints use an alias.** Shown to a domain owner releasing a new version: a held blueprint uses, as a word, an alias the new version introduces. |

## Invariants (I)

| Rule | Severity | What it means |
|---|---|---|
| `I008` | warning | **Blueprint-only keyword in a domain document.** A domain document's invariants block uses `affects` or `status`, which are for blueprints; they are ignored. |
| `I011` | error | **Invariant id used twice.** Two invariants of the document have the same id. |
| `I014` | warning | **Too many invariants.** The document has more than 100 invariants; the rest are not read. |
| `I015` | warning | **Statement too long.** An invariant's statement is longer than 300 characters. |
| `I016` | warning | **Invariant id is also a trace step id.** An invariant and a trace step under the same heading share an id, so the anchor `§slug#id` names both. |
| `I017` | error | **affects names an own invariant.** `affects` names an id without a document: the document's own invariants are written as invariant lines. |
| `I020` | error | **Invariant reference does not resolve.** A `guards` or `affects` line names an invariant that does not exist: an unknown id, `parent#<id>` with no parent or not among its invariants, or a domain that is not on the path from the pinned domain node to the root. |
| `I030` | warning | **No trace step guards the invariant.** The document has a trace, and no step lists the invariant (one it introduces or affects) under `guards`. |
| `I031` | warning | **Invariant not verified.** An invariant has no `verified-by`. |
| `I040` | info | **Referenced invariant removed in a newer domain version.** A blueprint references a domain invariant that a newer version of the domain no longer has. Also shown to the domain owner releasing that version, listing the held blueprints concerned. |

## Plan graph and goals (P)

| Rule | Severity | What it means |
|---|---|---|
| `P001` | error | **Goal id used twice.** Two goals or criteria of the document have the same id, or a goal or criterion has the id of an invariant of the document. |
| `P002` | error | **Criterion steps out of order.** A criterion's `given`, `when` and `then` lines are not in that order. |
| `P003` | warning | **Criterion without then.** A criterion has no `then` line. |
| `P004` | error | **realizes is not parent#<id>.** A goal's `realizes` does not name a goal of the parent plan node as `parent#<id>`, or a criterion's names neither that nor a goal of its own document. |
| `P010` | error | **Parent plan node not found.** `kalka_parent:` names a document that is not here, or a plan node further up names one that is not. |
| `P011` | info | **Plan graph not checked.** Only the files checked were read, with no folder to look plan nodes up in, and a parent, an ancestor or the children the rules need are not among them, so those rules were not run. |
| `P012` | error | **Parent cycle.** Following `kalka_parent:` from the document leads back to it. |
| `P020` | error | **realizes names no such goal.** `realizes parent#<id>` names no goal of the direct parent (a goal of a node further up is named through the levels between), or a criterion's `realizes <id>` names no goal of its own document. The parent must be here. |
| `P021` | error | **parent# with no parent.** The document writes `parent#<id>` and has no `kalka_parent:`. |
| `P022` | error | **covers names no such criterion.** A trace step's `covers` names an id that is not a criterion of the document. |
| `P023` | error | **Inherited invariant redefined.** The document defines an invariant with the id of one it inherits from a plan node above it. |
| `P030` | warning | **Goal unclaimed.** In an open plan node, no child goal and no criterion names the goal with `realizes`. |
| `P031` | error | **Closed with an undelivered goal.** The document is closed (`kalka_status: closed`) and a goal is unclaimed, or a claimant (a child goal or a criterion) is not delivered. |
| `P032` | info | **Goal coverage to decide.** With `kalka_breakdown: adopted`, a goal that is unclaimed or not delivered: a finding for a person to decide on, not a planning error. |

## Domain tree (D)

| Rule | Severity | What it means |
|---|---|---|
| `D001` | error | **Subdomain name not under its parent.** A domain node's name does not start with its parent's name and a dot. |
| `D002` | info | **Domain path not here, can't check.** A version on the path from a domain node to the root is not available, so the rules that need it were not run. |
| `D003` | error | **Domain parent cycle.** Following the domain nodes' `kalka_parent:` pins leads back to the node. |
| `D010` | error | **Subdomain redefines a term.** A subdomain defines a term, form or alias that a domain above it already defines. |
| `D011` | error | **Narrowing without refines.** A subdomain's invariant speaks only of terms its ancestors define, and none of its own, without saying which it narrows with `refines`. |
| `D012` | error | **refines names no ancestor term.** `refines` names a word that is not a term on the path from this domain to the root. |
| `D020` | error | **Word needs qualifying.** The document names several domain nodes, two of them define the same word differently, and the text uses it without saying which: `library.reservations#hold`. |
| `D021` | warning | **Inconsistent ancestor versions.** Two domain nodes the document names pin different versions of a common ancestor; the message shows both paths. |
| `D030` | info | **Parent domain newer.** A domain node pins an older version of its parent than the newest held. |
| `D031` | info | **Siblings share a term.** Two subdomains with the same parent define the same term the same way. |

## References to other documents (R)

| Rule | Severity | What it means |
|---|---|---|
| `R001` | info | **Domain version not here, can't check.** A domain version `kalka_domain:` names is not available, so the glossary, invariant and domain-tree rules that need it were not run. |
| `R002` | warning | **Malformed domain reference.** `kalka_domain:` in a blueprint is not `<team>/<domain>@v<N>`, or a list `[<team>/<domain>@v<N>, …]` of them. |
| `R003` | info | **Newer domain version.** A newer version of the referenced domain node is available, and it changes a term or an invariant the document uses. A newer version that changes nothing the document uses is not reported. |
| `R010` | warning | **Reference does not resolve.** With --with-plugins: a trace `ref` or an invariant's `verified-by` reference has no plugin or does not resolve. |
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

