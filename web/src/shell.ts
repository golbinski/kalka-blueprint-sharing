// The page knows it runs in the desktop window (spec 22) from a cookie the
// window's token link sets. There, links open in the system browser, the
// shell shows OS notifications, and the window settings apply.
import { openExternal } from "./api";

export function inDesktopWindow(): boolean {
  return (
    typeof document !== "undefined" &&
    /(?:^|;\s*)kalka_window=1(?:;|$)/.test(document.cookie)
  );
}

// externalHref returns the http(s) address a click on el should open
// outside, or null for links the page handles itself.
export function externalHref(el: Element | null): string | null {
  const a = el?.closest("a");
  if (!a) return null;
  const href = a.getAttribute("href") ?? "";
  if (!/^https?:\/\//i.test(href)) return null;
  try {
    if (new URL(href).origin === location.origin) return null;
  } catch {
    return null;
  }
  return href;
}

// routeLinksOutside makes links to other sites open in the system browser
// instead of inside the window.
export function routeLinksOutside(): void {
  if (!inDesktopWindow()) return;
  document.addEventListener("click", (e) => {
    const href = externalHref(e.target as Element | null);
    if (!href) return;
    e.preventDefault();
    openExternal(href).catch(() => {
      /* the daemon refused the address; nothing to open */
    });
  });
}
