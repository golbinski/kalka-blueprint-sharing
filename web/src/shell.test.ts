import { describe, expect, it } from "vitest";
import { externalHref } from "./shell";

// Minimal stand-in for an element inside a link.
function link(href: string | null): Element {
  const a = { getAttribute: (n: string) => (n === "href" ? href : null) };
  return {
    closest: (s: string) => (s === "a" ? a : null),
  } as unknown as Element;
}

describe("externalHref", () => {
  it("sends other sites outside and keeps the page routes", () => {
    expect(externalHref(link("https://example.org/x"))).toBe(
      "https://example.org/x",
    );
    expect(externalHref(link("HTTP://example.org"))).toBe("HTTP://example.org");
    expect(externalHref(link("#/team"))).toBeNull();
    expect(externalHref(link("mailto:a@b.example"))).toBeNull();
    expect(externalHref(link(null))).toBeNull();
    expect(externalHref(null)).toBeNull();
    expect(externalHref(link(location.origin + "/x"))).toBeNull();
  });
});
