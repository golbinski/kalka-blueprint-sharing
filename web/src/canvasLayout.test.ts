import { describe, expect, it } from "vitest";
import type { Doc, Entry, Section } from "./api";
import { layout, NOTE_X, SEC_X, noteText } from "./canvasLayout";
import { groupEntries } from "./entries";
import { plainText, refsIn } from "./markdown";

const sec = (
  text: string,
  slug: string,
  line: number,
  body: string,
  nested: string[] = [],
): Section => ({
  heading: { level: 2, text, slug, line },
  slugs: [slug, ...nested],
  markdown: `## ${text}\n\n${body}\n`,
  line,
});

const entry = (
  id: string,
  section: string | undefined,
  extra: Partial<Entry> = {},
): Entry => ({
  id,
  kind: "comment",
  author: "reader@library.example",
  timestamp: "2026-11-10T09:00:00Z",
  anchor: section ?? "",
  section,
  rev: "r1",
  body: "Text of " + id,
  valid: true,
  ...extra,
});

// A loan-renewal plan for a lending library.
function doc(): Doc {
  return {
    path: "/f/renewals.md",
    rel: "renewals.md",
    scope: "personal",
    kalka_id: "00000000-0000-4000-8000-000000000001",
    title: "Loan renewals",
    role: "reviewer",
    self: "reader@library.example",
    rev: "r1",
    sections: [
      { slugs: [], markdown: "# Loan renewals\n\nIntro.\n", line: 5 },
      sec(
        "Problem",
        "problem",
        8,
        "Members renew by phone. See [the desk script](gh:library/desk#README.md).",
      ),
      sec(
        "Approach",
        "approach",
        12,
        "Renew online.\n\n### Limits\n\nTwo renewals.",
        ["limits"],
      ),
      sec(
        "Rollout",
        "rollout",
        20,
        "```trace\nlanes: [business]\n```\n\nThen [the job](gh:library/jobs#renew.go) and [again](gh:library/jobs#renew.go).",
      ),
    ],
    headings: [],
    entries: [
      entry("e1", "problem"),
      entry("e2", "approach", {
        kind: "deviation",
        body: "We kept the phone line for a month.",
      }),
      entry("e3", "problem", { reply_to: "e1", body: "re: e1\nAgreed." }),
      entry("e4", undefined, { body: "Looks good overall." }),
      entry("e5", "limits"),
      entry("e6", "gone", { orphaned: true }),
      entry("e7", "problem", { reply_to: "e3" }),
      entry("e8", "problem", { reply_to: "missing" }),
    ],
    problems: [],
    invariants: [],
    glossary: { state: 'none', terms: [] },
    traces: [
      { index: 0, line: 22, source: "", version: 1, divergences: [], graph: 0 },
    ],
    refs: {},
  };
}

describe("canvas layout", () => {
  it("shows every section and every entry of the linear view", () => {
    const d = doc();
    const l = layout(d);
    expect(l.sections.map((s) => s.title)).toEqual([
      "Before the first heading",
      "Problem",
      "Approach",
      "Rollout",
      "General",
      "Orphaned",
    ]);
    expect(l.notes.map((n) => n.entry.id).sort()).toEqual(
      d.entries.map((e) => e.id).sort(),
    );
    // Each note sits beside the card the linear view puts it under.
    const g = groupEntries(d);
    const cardOf = Object.fromEntries(l.notes.map((n) => [n.entry.id, n.card]));
    for (const e of g.bySlug.get("problem")!) expect(cardOf[e.id]).toBe("s1");
    expect(cardOf.e5).toBe("s2"); // anchored to a nested heading
    expect(cardOf.e4).toBe("general");
    expect(cardOf.e6).toBe("orphaned");
    expect(cardOf.e8).toBe("s1"); // reply to an entry not in the file: top level, as in the linear view
  });

  it("threads replies under the note they answer", () => {
    const l = layout(doc());
    const at = Object.fromEntries(l.notes.map((n) => [n.entry.id, n]));
    expect([at.e1.depth, at.e3.depth, at.e7.depth]).toEqual([0, 1, 2]);
    expect(at.e3.y).toBeGreaterThan(at.e1.y);
    expect(at.e7.x).toBe(NOTE_X + 48);
    expect(
      l.edges.filter((e) => e.kind === "reply").map((e) => `${e.from}>${e.to}`),
    ).toEqual(["e1>e3", "e3>e7"]);
    expect(
      l.edges
        .filter((e) => e.kind === "note" && e.from === "s1")
        .map((e) => e.to),
    ).toEqual(["e1", "e8"]);
  });

  it("places references beside their section, once each", () => {
    const l = layout(doc());
    expect(l.refs.map((r) => `${r.card}:${r.ref}`)).toEqual([
      "s1:gh:library/desk#README.md",
      "s3:gh:library/jobs#renew.go",
    ]);
    const s3 = l.sections.find((s) => s.key === "s3")!;
    expect(s3.traces).toBe(1);
    expect(l.sections.find((s) => s.key === "s1")!.entries).toBe(4);
    expect(l.refs[1].y).toBe(s3.y);
  });

  it("keeps reading order and never overlaps rows", () => {
    const l = layout(doc());
    for (let i = 1; i < l.sections.length; i++) {
      const prev = l.sections[i - 1];
      const bottom = Math.max(
        prev.y + prev.h,
        ...l.notes.filter((n) => n.card === prev.key).map((n) => n.y + n.h),
      );
      expect(l.sections[i].y).toBeGreaterThan(bottom);
    }
    for (const s of l.sections) expect(s.x).toBe(SEC_X);
    for (const n of l.notes) expect(n.y + n.h).toBeLessThanOrEqual(l.height);
  });

  it("gives the same layout for the same input", () => {
    const a = JSON.stringify(layout(doc()));
    const b = JSON.stringify(layout(structuredClone(doc())));
    expect(b).toBe(a);
  });

  it("has a General card even without entries", () => {
    const d = doc();
    d.entries = [];
    const l = layout(d);
    expect(l.sections.map((s) => s.key)).toEqual([
      "s0",
      "s1",
      "s2",
      "s3",
      "general",
    ]);
    expect(l.notes).toEqual([]);
  });
});

describe("note text", () => {
  it("reads special entries in words", () => {
    expect(
      noteText(
        entry("x", undefined, {
          kind: "budded",
          budded_seed: "id",
          budded_title: "Holds queue",
        }),
      ),
    ).toBe("Split off a seed: Holds queue");
    expect(
      noteText(
        entry("x", undefined, {
          kind: "release",
          body: `v3 · plan:${"a".repeat(64)} · owner:desk@library.example`,
          release: {
            version: 3,
            plan: "a".repeat(64),
            owner: "desk@library.example",
          },
        }),
      ),
    ).toBe("Released version 3, owner desk@library.example");
  });
});

describe("markdown helpers", () => {
  it("lists plugin references, not web links", () => {
    expect(
      refsIn(
        "[a](gh:x#y) [b](https://example.org) [c](gh:x#y) [d](jira:LIB-4)",
      ),
    ).toEqual(["gh:x#y", "jira:LIB-4"]);
  });
  it("reads Markdown as plain text", () => {
    expect(
      plainText(
        "## Head\n\n- **Two** renewals, see [desk](gh:a#b).\n\n```trace\nlanes: [x]\n```\n> quoted `code`",
      ),
    ).toBe("Two renewals, see desk. [trace] quoted code");
  });
});
