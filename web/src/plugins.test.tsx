// Plugins in Settings (spec 10.6, 11.1): the list, each plugin's state,
// and the settings form, with the token handled as a secret.
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, api, type PluginList, type PluginSettings } from './api'
import { PluginsSection } from './Plugins'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const SECRET = 'atl-token-0123456789'
const list: PluginList = {
  dirs: { config: '/home/ewa/.config/kalka/plugins', bundled: '/opt/kalka' },
  plugins: [
    { scheme: 'jira', name: 'jira', version: '0.2.0', file: '/opt/kalka/kalka-plugin-jira', source: 'bundled', shadowed: [],
      capabilities: { resolve: true, issue: { meta: true, export: { attachment: true } }, settings: { apply: true, clear: true } } },
    { scheme: 'gh', name: 'github', version: '0.2.0', file: '/home/ewa/.config/kalka/plugins/kalka-plugin-gh', source: 'config',
      shadowed: ['/usr/bin/kalka-plugin-gh'], capabilities: { resolve: true, settings: {} } },
    { scheme: 'lib', file: '/home/ewa/bin/kalka-plugin-lib', source: 'path', shadowed: [], problem: 'plugin speaks protocol 1, kalka speaks 2' },
  ],
}
const jira = (over: Partial<PluginSettings> = {}, set = false): PluginSettings => ({
  scheme: 'jira',
  status: set ? { state: 'ready', message: 'https://parcels.example.net as ewa@parcels.example', checked: false } : { state: 'needs-setup', message: 'Not connected to Jira.', checked: false },
  fields: [
    { id: 'flavor', kind: 'choice', label: 'Jira', required: true, value: 'cloud', choices: [{ value: 'cloud', label: 'Jira Cloud' }, { value: 'datacenter', label: 'Jira Data Center or Server' }] },
    { id: 'site', kind: 'url', label: 'Site <img src=x onerror=alert(1)>', required: true, value: set ? 'https://parcels.example.net' : undefined },
    { id: 'email', kind: 'email', label: 'Account email', required: true, value: set ? 'ewa@parcels.example' : undefined, when: { field: 'flavor', is: ['cloud'] } },
    { id: 'token', kind: 'secret', label: 'API token', required: true, set, help: 'An API token from id.atlassian.com.' },
  ],
  hint: null,
  incomplete: false,
  can: { apply: true, clear: true },
  ...over,
})
const gh: PluginSettings = {
  scheme: 'gh', status: { state: 'ready', message: "Uses the GitHub CLI's login.", checked: false }, fields: [],
  hint: { text: "Log in with gh.", command: 'gh auth login', url: 'https://cli.github.com' }, incomplete: false, can: { apply: false, clear: false },
}

let host: HTMLDivElement
let root: Root
const render = async (el: React.ReactNode) => {
  await act(async () => root.render(el))
}
const flush = () => act(async () => {})
const q = <T extends Element = HTMLElement>(sel: string) => host.querySelector<T>(sel)
const button = (text: string) => [...host.querySelectorAll('button')].find((b) => b.textContent === text) as HTMLButtonElement | undefined
const section = (scheme: string) => [...host.querySelectorAll('section')].find((s) => s.querySelector('h3')?.textContent?.startsWith(scheme + ':'))!
const type = async (el: HTMLInputElement, v: string) => {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!
  await act(async () => {
    set.call(el, v)
    el.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
const click = (el: Element) => act(async () => void (el as HTMLElement).click())

let settings: Record<string, PluginSettings>
beforeEach(() => {
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
  settings = { jira: jira(), gh }
  vi.spyOn(api, 'plugins').mockResolvedValue(list)
  vi.spyOn(api, 'pluginSettings').mockImplementation(async (s) => settings[s])
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
  vi.restoreAllMocks()
})

describe('PluginsSection', () => {
  it('lists each plugin with where it was found and its state', async () => {
    await render(<PluginsSection tick={0} />)
    await flush()
    const text = host.textContent!
    expect(section('jira').textContent).toContain('shipped with kalka')
    expect(section('jira').textContent).toContain('References, Issue details, Export')
    expect(section('jira').querySelector('[role=status]')!.textContent).toBe('Not set up · Not connected to Jira.')
    expect(section('gh').textContent).toContain('from your plugins folder: /home/ewa/.config/kalka/plugins/kalka-plugin-gh')
    expect(section('gh').textContent).toContain('Used instead of /usr/bin/kalka-plugin-gh.')
    expect(section('gh').textContent).toContain('Ready (not checked)')
    expect(section('lib').textContent).toContain('found on PATH: /home/ewa/bin/kalka-plugin-lib')
    expect(section('lib').textContent).toContain('Problem · plugin speaks protocol 1')
    expect(section('lib').querySelector('button')).toBeNull()
    // gh: nothing to set; the hint's command is text with Copy, not a link.
    expect(section('gh').querySelector('.plugin-hint code')!.textContent).toBe('gh auth login')
    expect([...section('gh').querySelectorAll('a')].map((a) => a.textContent)).toEqual(['https://cli.github.com'])
    expect([...section('gh').querySelectorAll('button')].map((b) => b.textContent)).toEqual(['Copy', 'Check'])
    expect(text).not.toContain('Log out…')
    expect(section('jira').getAttribute('aria-labelledby')).toBe(section('jira').querySelector('h3')!.id)
  })

  it('says where it looked when there are none', async () => {
    vi.mocked(api.plugins).mockResolvedValue({ ...list, plugins: [] })
    await render(<PluginsSection tick={0} />)
    await flush()
    expect(host.textContent).toContain('No plugins found. kalka looks in /home/ewa/.config/kalka/plugins, beside itself (/opt/kalka)')
  })

  it('shows a failed settings op and an incomplete form', async () => {
    vi.mocked(api.pluginSettings).mockImplementation(async (s) => {
      if (s === 'jira') throw new ApiError('kalka-plugin-jira failed: timed out after 10s', 502, 'plugin-failed')
      return { ...gh, incomplete: true, can: { apply: false, clear: false } }
    })
    await render(<PluginsSection tick={0} />)
    await flush()
    expect(section('jira').querySelector('[role=status]')!.textContent).toContain('Problem · kalka-plugin-jira failed')
    expect(button('Check')).toBeDefined()
    expect(section('gh').textContent).toContain('This plugin has settings this kalka cannot show. Update kalka.')
  })

  it('sets up a plugin: when rules, required fields, the secret only under secrets', async () => {
    const apply = vi.spyOn(api, 'applyPluginSettings').mockImplementation(async () => jira({}, true))
    await render(<PluginsSection tick={0} />)
    await flush()
    await click(button('Set up…')!)
    const form = q('form')!
    expect(form.querySelector('legend')!.textContent).toBe('jira')
    // The label is text, not markup.
    expect(form.querySelector('img')).toBeNull()
    expect(form.textContent).toContain('Site <img src=x onerror=alert(1)>')
    expect(document.activeElement).toBe(form.querySelector('input'))
    const save = button('Save')!
    expect(save.disabled).toBe(true)
    const site = form.querySelector<HTMLInputElement>('input[type=url]')!
    const token = form.querySelector<HTMLInputElement>('input[type=password]')!
    expect(token.value).toBe('')
    expect(token.getAttribute('autocomplete')).toBe('off')
    expect(token.getAttribute('aria-describedby')).toBeTruthy()
    await type(site, 'https://parcels.example.net')
    await type(form.querySelector<HTMLInputElement>('input[type=email]')!, 'ewa@parcels.example')
    expect(save.disabled).toBe(true)
    await type(token, SECRET)
    expect(save.disabled).toBe(false)
    // Data Center hides the email, which is then not sent.
    await click(form.querySelector('input[value=datacenter]')!)
    expect(form.querySelector('input[type=email]')).toBeNull()
    await click(save)
    await flush()
    expect(apply).toHaveBeenCalledWith('jira', { flavor: 'datacenter', site: 'https://parcels.example.net' }, { token: SECRET })
    expect(q('form')).toBeNull()
    expect(host.innerHTML).not.toContain(SECRET)
    expect(section('jira').querySelector('[role=status]')!.textContent).toContain('Ready (not checked)')
    expect(document.activeElement?.textContent).toBe('Change…')
  })

  it('keeps a stored secret when nothing is typed', async () => {
    settings.jira = jira({}, true)
    const apply = vi.spyOn(api, 'applyPluginSettings').mockImplementation(async () => jira({}, true))
    await render(<PluginsSection tick={0} />)
    await flush()
    await click(button('Change…')!)
    const token = q<HTMLInputElement>('input[type=password]')!
    expect(token.value).toBe('')
    expect(token.placeholder).toBe('Stored. Leave empty to keep it.')
    expect(button('Save')!.disabled).toBe(false)
    await type(q<HTMLInputElement>('input[type=email]')!, 'ewa.nowak@parcels.example')
    await click(button('Save')!)
    await flush()
    expect(apply).toHaveBeenCalledWith('jira', { email: 'ewa.nowak@parcels.example' }, {})
  })

  it('keeps what was typed after a refusal, and points at the field', async () => {
    vi.spyOn(api, 'applyPluginSettings').mockRejectedValue(
      new ApiError('Jira did not accept the credentials.', 422, 'plugin-rejected', { token: 'Check that the token is current.' }),
    )
    await render(<PluginsSection tick={0} />)
    await flush()
    await click(button('Set up…')!)
    await type(q<HTMLInputElement>('input[type=url]')!, 'https://parcels.example.net')
    await type(q<HTMLInputElement>('input[type=email]')!, 'ewa@parcels.example')
    await type(q<HTMLInputElement>('input[type=password]')!, SECRET)
    await click(button('Save')!)
    await flush()
    await act(() => new Promise((r) => setTimeout(r, 0)))
    expect(q('[role=alert]')!.textContent).toBe('Jira did not accept the credentials.')
    const token = q<HTMLInputElement>('input[type=password]')!
    expect(token.value).toBe(SECRET)
    expect(token.getAttribute('aria-invalid')).toBe('true')
    expect(q('form')!.textContent).toContain('Check that the token is current.')
    expect(document.activeElement).toBe(token)
    // Escape cancels and drops it.
    await act(async () => void token.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })))
    expect(q('form')).toBeNull()
    expect(host.innerHTML).not.toContain(SECRET)
  })

  it('confirms before logging out', async () => {
    settings.jira = jira({}, true)
    const clear = vi.spyOn(api, 'clearPluginSettings').mockResolvedValue(jira())
    await render(<PluginsSection tick={0} />)
    await flush()
    await click(button('Log out…')!)
    expect(clear).not.toHaveBeenCalled()
    expect(host.textContent).toContain('Remove the stored settings of jira?')
    await click(button('Log out')!)
    await flush()
    expect(clear).toHaveBeenCalledWith('jira')
    expect(section('jira').querySelector('[role=status]')!.textContent).toContain('Not set up')
  })

  it('checks on request', async () => {
    await render(<PluginsSection tick={0} />)
    await flush()
    await click([...section('gh').querySelectorAll('button')].find((b) => b.textContent === 'Check')!)
    await flush()
    expect(api.pluginSettings).toHaveBeenCalledWith('gh', true)
  })
})
