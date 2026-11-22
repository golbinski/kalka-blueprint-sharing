# The kalka language, version 2

kalka reads meaning from two places in a Markdown document: the frontmatter, and typed blocks, the fenced code blocks whose info string names a kind kalka knows (`goals`, `invariants`, `glossary`, `trace`). Everything else is prose, which kalka renders and links but never interprets. This document defines the language every typed block is written in: its lexis, its grammar, the keyword table, references, ids and anchors, and one chapter per block. `specification.md` section 6 says what kalka does with the blocks; `trace.md` defines the trace block in full.

Version 2 replaces the YAML of version 1 for every block. There is no conversion and no compatibility: a `v1` block is shown as code (S012).

## 1. Design

- **One line, one value.** A list is a repeated keyword. There is no nesting beyond one indented level, no line continuation, no quoting and no escaping.
- **Readable as plain text.** A block reads as sentences: `term copy: One physical item of a title that can be lent`.
- **Hard to drift from.** Keywords are lower case, singular whole words, only at the start of a line. Familiar formats an author or an agent might slip into, Gherkin (`Scenario:`, `Given`) and YAML (`- id:`, `lanes:`), are recognised and reported with the v2 form to write (S010).
- **Every mistake names its fix.** Each lexical rule says what to write instead, and most carry the replacement line.

## 2. The fence

```ebnf
info    = kind , [ " " , version ] ;
kind    = "goals" | "invariants" | "glossary" | "trace" ;
version = "v" , nonzero , { digit } ;
```

- `v2` is the version this document defines. It is the same number for every block: the version of the language, not of one block.
- A fence with no version is read as v2 with warning S013.
- `v1` (the retired YAML form), a newer version, or a second word that is not a version is S012: the block is shown as code and not read. A newer version says "needs a newer kalka".
- A block inside a list item or a quote is not a typed block: it stays code.

## 3. Lines

Inside the fence every line is one of:

```ebnf
block     = { line } ;
line      = blank | header , nl | attribute , nl ;
blank     = { " " } , nl ;
header    = item | section | decl ;                   (* in column 0 *)
item      = keyword , " " , name , ": " , text ;       (* step B1: A member returns a copy *)
section   = keyword , " " , text ;                     (* lane business *)
decl      = keyword , " " , ref ;                      (* affects library#L1 *)
attribute = "  " , keyword , " " , value ;             (* exactly two spaces *)
value     = text | ref | ref , ": " , label | term ;
keyword   = lower , { lower | "-" } ;
name      = id | term ;
id        = letter , { letter | digit | "-" | "_" } ;   (* at most 32 characters *)
term      = word , { " " , word } ;                    (* ends at the first ": " *)
ref       = { any character but space } ;
text      = any characters, not empty ;
```

- A header line starts in column 0; an attribute line is indented by exactly two spaces and belongs to the item above it. There is no third level (S004), and an attribute with no item above it is S006.
- The keyword is the first word. Never put a colon after it: `why because`, not `why: because` (S003). Capitals are S002.
- The value is the rest of the line. Free text may hold commas, colons, quotes and `#`; leading and trailing spaces are dropped. An empty value is S007.
- A reference has no spaces (S009). A labelled reference (`next C3: queue not empty`) is a reference, `: `, and a free-text label.
- Tabs are S001; the fix writes two spaces.
- There are no comments (S011): a line starting with `#` or `//` is an error. Say it in the prose around the block.
- A keyword of your own starts with `x-` and is ignored without a warning. Any other unknown keyword is ignored with warning S020, which suggests the nearest known one.
- A keyword with multiplicity one, written twice under one item, is S021. A keyword with a fixed set of values (`status`, `change`) given another word is S022.

## 4. The keyword table

| Block | Header | Kind | Attributes (`*` repeatable) |
|---|---|---|---|
| `goals` | `goal <id>: <text>` | item | `realizes*` |
| | `criterion <id>: <text>` | item | `realizes*`, `given*`, `when*`, `then*` |
| `invariants` | `invariant <id>: <statement>` | item | `why`, `verified-by*`, `refines*`, `status` |
| | `affects <ref>` | declaration | none |
| `glossary` | `term <term>: <definition>` | item | `form*`, `alias*`, `see*`, `context` |
| `trace` | `lane <name>` | section | none |
| | `step <id>: <text>` | item | `next*`, `back*`, `realizes*`, `covers*`, `ref`, `guards*`, `why`, `change` |

| Attribute | Value | Meaning |
|---|---|---|
| `realizes` | ref | In `goals`: the goal it serves, `parent#<id>`; a criterion may also name a goal of its own document. In `trace`: a step in a lane above. |
| `given`, `when`, `then` | text | A criterion's precondition, action and observed outcome, in this order (P002). Repeat a keyword for a second line; there is no `and`. |
| `why` | text | Why it holds, or why the step exists. |
| `verified-by` | text | How the invariant is checked: a reference (`gh:…`) or a check a person makes, starting `Reviewer:`. |
| `refines` | term | An inherited term the invariant narrows (specification 6.12). |
| `status` | `new`, `changed`, `removed` | What the plan does to the invariant. Not in a domain document. |
| `form` | text | Another form of the term (a plural). |
| `alias` | text | A word people also use that the plan should not (G020). |
| `see` | term | A related term of the same glossary or of a domain above. |
| `context` | text | The bounded context. |
| `next`, `back` | ref, optional label | A following step, or a step the flow returns to. |
| `covers` | ref | An acceptance criterion of the document the step demonstrates. |
| `ref` | ref | `scheme:payload`, resolved by a plugin. |
| `guards` | ref | An invariant the step keeps true. |
| `change` | `new`, `modified`, `removed` | What the plan does to the step. |

## 5. References

| Form | Names | Example |
|---|---|---|
| `<id>` | an item of the same document | `realizes B1`, `guards N1`, `covers AC1` |
| `parent#<id>` | a goal or invariant of the parent plan node (`kalka_parent`) | `realizes parent#G1`, `affects parent#E1` |
| `<domain>#<id>` | an invariant of a domain node on the document's domain path | `guards library.reservations#R1` |
| `<domain>#<term>` | a term, qualified, in prose, where two named domain nodes define it differently (D020) | `library.fines#hold` |

`parent` is reserved: no domain may be named so (F012). A domain name is a path of lower-case segments joined by dots, `library.reservations`; a subdomain's name starts with its parent's (D001).

## 6. Ids and anchors

An id starts with a letter and uses letters, digits, `-` and `_`, at most 32 characters (S030). Goal, criterion and invariant ids are unique across the document (P001, I011); step ids are unique within their trace block (T031), and should be unique under one heading (T120). Ids are permanent: reviewers' comments are anchored to `§<heading slug>#<id>`, so keep an id when its text changes, and never give a removed item's id to another.

## 7. The blocks

### 7.1 goals

Goals of a plan node, and acceptance criteria of the blueprints below it (specification 6.18).

```goals v2
goal G1: The queue head learns that a copy is waiting for them
goal G2: A held copy is not lent to anyone else

criterion AC1: The queue head is notified exactly once
  realizes G1
  given a copy whose title has two open reservations
  when the copy is returned
  then the queue head gets one email
  then the second member gets nothing
```

A goal realizes only goals of the parent (P004); a criterion realizes a goal of the parent or of its own document. A criterion without `then` is P003. States are computed, never written: unclaimed, claimed, ready, delivered.

### 7.2 invariants

What must hold (specification 6.15).

```invariants v2
affects library#L1

invariant N1: A copy on hold is never put back on the shelf
  why the queue head would lose it
  verified-by gh:test/holds_test.py#L62-L90
  status new
```

`affects` names an invariant of another document the plan touches: `<domain>#<id>` or `parent#<id>`. A block may hold only `affects` lines.

### 7.3 glossary

The shared words of a domain, or of a plan that names no domain (specification 6.16).

```glossary v2
term queue head: The member whose reservation is first in the queue
  form queue heads
  alias next in line
  see member
  context reservations
```

A term's name may hold spaces and ends at the first `: `. A word names one entry: the same term, form or alias in two entries is G012.

### 7.4 trace

One process at two or more levels, most abstract first, and the links between them. Defined in full in [`trace.md`](trace.md).

```trace v2
lane business
step B1: A member returns a copy
  next B2
step B2: The queue head is told the copy waits for them
  covers AC1

lane code
step C1: Record the return
  realizes B1
  next C2
  ref gh:src/loans/returns.py#L30-L77
step C2: Send the hold email
  realizes B2
  ref gh:src/holds/notify.py#L9-L40
```

## 8. Lexical rules

| Id | Severity | Condition |
|---|---|---|
| S001 | error | A tab. Fix: two spaces. |
| S002 | error | A keyword with capitals. Fix: lower case. |
| S003 | error | A colon after a keyword. Fix: drop it. |
| S004 | error | An indentation other than 0 or 2 spaces. |
| S005 | error | A header line without its parts: `step <id>: <text>`, `lane <name>`, `affects <ref>`. |
| S006 | error | An attribute line with no item above it. |
| S007 | error | A keyword with no value. |
| S008 | error | A keyword on the wrong level: an attribute in column 0, a header indented. |
| S009 | error | A reference with spaces; one reference per line. |
| S010 | error | Gherkin or YAML: `Scenario:`, `Given`, `And`, `- id:`, `lanes:`. The message says the v2 form. |
| S011 | error | A comment line. |
| S012 | error | A fence version this kalka does not read: v1, a newer one, or not a version. |
| S013 | warning | A fence with no version; read as v2. |
| S020 | warning | An unknown keyword, not starting with `x-`; ignored, with the nearest known one suggested. |
| S021 | error | A keyword of multiplicity one written twice under one item. |
| S022 | error | A value that is not one of the allowed words. |
| S030 | error | An id that does not match the id grammar. |
| S031 | error | A block with no items. |

A line with a lexical error is reported and skipped; a trace block with one is shown as code. Each block's own rules (G, I, P, T) and the rules across documents (D, R) are listed in [`rules.md`](rules.md).
