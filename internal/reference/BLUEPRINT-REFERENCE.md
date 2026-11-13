---
name: kalka-blueprint
description: How to write a blueprint (a Markdown implementation plan reviewed with kalka) that agents can act on safely. Use when writing, extending or implementing a blueprint, or when recording where an implementation departed from one.
kalka_reference: kalka-blueprint@v1
---

# Writing blueprints agents can use

A *blueprint* is one Markdown file describing an implementation plan. People and agents write it with their own tools; kalka passes it between teammates for review and collects their comments in a `## Review log` at the end of the file.

This reference describes what a blueprint useful to agents usually contains. It is guidance, not a template you must fill: a short plan for a small change may need two of these sections, and kalka accepts any Markdown. The examples come from an invented parcel-delivery service.

## Opting in

Add one line to the blueprint's frontmatter:

```yaml
reference: kalka-blueprint@v1
```

kalka then shows soft hints, such as "no verification plan", which anyone can dismiss. Documents without this line get no hints. Hints never block a sync and never fail `kalka check`.

## Rules for agents working on a blueprint

- **The body belongs to the owner.** The frontmatter `owner:` names them. If you are not acting for the owner, do not edit the body: add a comment instead (`kalka comment`).
- **Never edit the review log by hand.** Entries are signed and hashed; an edited entry becomes invalid. Add entries with `kalka comment <file> --anchor <slug> -m "<text>"`.
- **Leave unknown frontmatter alone.** Keys you do not understand belong to someone's pipeline.
- **Stay inside the scope.** Work that does not fit becomes a seed, not a larger change (see "When to split off a seed").
- **Do not "clean up" critical sections.** They look wrong on purpose. Read their reasoning first and ask a human.

## The sections

Use these headings (any level) so that people, agents and kalka's hints find them. Each one says why it helps an agent.

### Domain reference

Which domain document, and which version of it, the plan was written against. Put it in the frontmatter, and repeat it in prose if the plan relies on particular terms:

```yaml
domain: parcels/delivery@v7
```

*Why agents benefit:* the domain document defines the team's words (what a "consignment" is, when a parcel counts as "delivered"). An agent that reads the right version uses the same names and respects the same invariants as the rest of the code. kalka shows when a newer version exists, so the plan's author can decide whether it matters.

### Scope

The bounded contexts, modules and interfaces the change is expected to touch, and anything it must not touch.

> Touches the `redelivery` module and the `DeliveryAttempt` table. Does not change the courier app's API or the tracking page.

*Why agents benefit:* an explicit boundary lets an agent tell "part of this plan" from "a new idea". When the work turns out to need more, the agent stops and proposes a seed instead of quietly growing the change, which keeps the review the reviewers agreed to.

### Verification plan

Invariants that must hold, edge cases, how each will be tested, and what a human reviewer must specifically check. Write it while planning, not while reviewing.

> - A parcel is never scheduled for more than three redelivery attempts.
> - A recipient who reschedules during an attempt in progress gets the new slot, not a duplicate.
> - Reviewer: check the migration on a copy of last week's attempts table; it rewrites 2 million rows.

*Why agents benefit:* an agent cannot know which properties matter most to the team. A verification plan written up front turns them into tests the agent writes first, and tells the reviewer where to spend their attention instead of rereading everything.

### Critical sections

Places where the design deliberately departs from the clean model, for example for performance, compatibility or an external constraint, with the reasoning.

> The attempt counter is denormalized onto the parcel row. Joining the attempts table on every scan of the sorting belt was too slow (p99 above the belt's 40 ms budget). Keep the two in sync in the same transaction; do not replace the counter with a join.

*Why agents benefit:* agents are trained to remove duplication and restore the "proper" design. Here that would reintroduce the problem the design avoids. Naming these places, and requiring human review there, prevents a well-meant cleanup from undoing a decision.

### Operations

How the change fails in production, what to watch, and how to roll it back.

> - Failure: the scheduler stops creating attempts. Watch `redelivery_attempts_created` per hour; alert below 50 % of the same hour last week.
> - Rollback: switch off the `redelivery-v2` flag. The migration is additive, so no schema rollback is needed.

*Why agents benefit:* an agent implementing the plan adds the metrics, logs and flags operations will need, instead of leaving them for later. A reviewer can check that the rollback really works without the author present.

### Deviation log

Where the implementation departed from the plan as written. This is not a section in the body: it is the set of `deviation` entries in the review log, which kalka keeps in order and never loses.

Record a deviation as soon as it happens:

```sh
kalka comment plan.md --anchor scope --kind deviation \
  -m "Also changed the courier app's reschedule endpoint: it validated the old three-slot rule."
```

*Why agents benefit:* the plan stays the agreed reference, and the log says what changed and why. The next agent, or the reviewer, sees both, instead of a plan silently rewritten to match the code.

## When to split off a seed

A *seed* is a small blueprint (`status: seed`) holding an idea split off from a plan that is growing. Split one off when:

- the work needs a module outside the plan's scope;
- a deviation turns out to be a feature of its own;
- a reviewer's comment raises something worth doing, but not now.

In kalka, "Create seed" on a deviation entry, or `kalka seed <file> --from <entry id> --title "<title>"`, writes the seed with its problem and why it was split off, and records a `budded` entry in the parent so the two stay linked. The seed waits in the team's pre-backlog until its owner promotes it.

## A skeleton

```markdown
---
owner: dana@example.org
reference: kalka-blueprint@v1
domain: parcels/delivery@v7
---

# Redelivery scheduling

## Context
## Scope
## Plan
## Verification plan
## Critical sections
## Operations
```

The review log is added by kalka when the first comment arrives.
