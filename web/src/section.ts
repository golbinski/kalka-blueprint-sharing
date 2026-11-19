// Which section a document belongs to (spec 11.1): Blueprints, Seeds or
// Domains. One rule decides the top bar's highlight, the breadcrumb, the
// document rail and what the Blueprints list shows, so a document is
// always highlighted in the section whose list holds it.
import type { BlueprintInfo, Doc } from './api'
import { PERSONAL } from './scope'

export type Section = 'list' | 'seeds' | 'domains'

export const SECTION_LABEL: Record<Section, string> = { list: 'Blueprints', seeds: 'Seeds', domains: 'Domains' }

/** What the rule reads, the same on a list row and on an open document. */
interface Kind {
  /** A valid domain name: kalka_kind is domain and kalka_domain names it. */
  domain?: string
  seed?: boolean
  duplicate?: boolean
  personal: boolean
}

// sectionFor mirrors the daemon's lists: Domains holds the domain
// documents with a valid name that are not duplicates, in a team only
// (Personal has no Domains); Seeds holds every kalka_status: seed;
// Blueprints the rest.
function sectionFor(k: Kind): Section {
  if (k.domain && !k.duplicate && !k.personal) return 'domains'
  if (k.seed) return 'seeds'
  return 'list'
}

/** sectionOf is the section of an open document. */
export function sectionOf(doc: Doc): Section {
  return sectionFor({ domain: doc.domain?.name, seed: !!doc.seed, duplicate: doc.duplicate, personal: doc.scope === PERSONAL })
}

/** sectionOfRow is the section of a row of the list of scope. */
export function sectionOfRow(b: BlueprintInfo, scope: string): Section {
  return sectionFor({ domain: b.domain, seed: b.seed, duplicate: b.duplicate, personal: scope === PERSONAL })
}

// activeSection is what the top bar highlights: in a document, the section
// it belongs to (nothing while it loads); elsewhere the view on screen.
export function activeSection(view: string, doc: Doc | null): string {
  if (view === 'doc' || view === 'trace' || view === 'canvas') return doc ? sectionOf(doc) : ''
  return view
}

/** blueprintRows is what the Blueprints list shows, in its order: most recent activity first. */
export function blueprintRows(list: BlueprintInfo[], scope: string): BlueprintInfo[] {
  return list.filter((b) => sectionOfRow(b, scope) === 'list').sort((a, b) => (b.activity ?? b.modified).localeCompare(a.activity ?? a.modified))
}
