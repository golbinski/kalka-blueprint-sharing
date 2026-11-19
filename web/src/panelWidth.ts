// Width of the trace side panel: dragged, widened or set with the keys, kept
// between visits.
export const MIN_PANEL = 320
export const DEFAULT_PANEL = 400
const KEY = 'kalka.tracePanelWidth'

// clampWidth keeps w within [MIN_PANEL, 75% of the viewport]; a viewport too
// small for that range gets MIN_PANEL.
export function clampWidth(w: number, viewport: number): number {
  const max = Math.max(MIN_PANEL, Math.floor(viewport * 0.75))
  if (!Number.isFinite(w)) return Math.min(DEFAULT_PANEL, max)
  return Math.min(max, Math.max(MIN_PANEL, Math.round(w)))
}

// widePreset is the "widen" width: 60% of the viewport.
export const widePreset = (viewport: number) => clampWidth(viewport * 0.6, viewport)

export function loadWidth(store: Pick<Storage, 'getItem'> | undefined = safe()): number {
  const n = Number(store?.getItem(KEY))
  return Number.isFinite(n) && n > 0 ? n : DEFAULT_PANEL
}

export function saveWidth(w: number, store: Pick<Storage, 'setItem'> | undefined = safe()) {
  try {
    store?.setItem(KEY, String(Math.round(w)))
  } catch {
    // storage may be full or blocked; the width just is not remembered
  }
}

function safe(): Storage | undefined {
  try {
    return globalThis.localStorage
  } catch {
    return undefined
  }
}
