import { describe, expect, it } from 'vitest'
import { DEFAULT_PANEL, clampWidth, loadWidth, saveWidth, widePreset } from './panelWidth'

const mem = () => {
  const m = new Map<string, string>()
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) }
}

describe('clampWidth', () => {
  it('stays within 320px and 75% of the viewport', () => {
    expect(clampWidth(100, 1600)).toBe(320)
    expect(clampWidth(5000, 1600)).toBe(1200)
    expect(clampWidth(500.4, 1600)).toBe(500)
  })
  it('never goes below the minimum on small viewports', () => {
    expect(clampWidth(900, 400)).toBe(320)
  })
  it('falls back to the default for nonsense', () => {
    expect(clampWidth(NaN, 1600)).toBe(DEFAULT_PANEL)
  })
  it('widens to 60% of the viewport', () => {
    expect(widePreset(1500)).toBe(900)
  })
})

describe('persistence', () => {
  it('round-trips and defaults', () => {
    const s = mem()
    expect(loadWidth(s)).toBe(DEFAULT_PANEL)
    saveWidth(640, s)
    expect(loadWidth(s)).toBe(640)
    s.setItem('kalka.tracePanelWidth', 'junk')
    expect(loadWidth(s)).toBe(DEFAULT_PANEL)
  })
})
