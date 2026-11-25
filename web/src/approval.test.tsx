// Approval in the app (spec 6.18, 11.1): the state in the header, Approve
// vN for the owner only when there is something to approve, the open
// items as information, and the optional attach question after it.
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, type Doc } from './api'
import { approvalText, approveRole, ApproveDialog, openItems } from './approval'
import { DocHeader } from './DocHeader'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

let host: HTMLDivElement
let root: Root
const render = async (el: React.ReactNode) => {
  await act(async () => root.render(el))
}
beforeEach(() => {
  // jsdom has no modal dialogs.
  HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
    this.open = true
  }
  HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
    this.open = false
  }
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
  vi.restoreAllMocks()
})

const doc = (more: Partial<Doc> = {}, team: Partial<NonNullable<Doc['team']>> = {}): Doc =>
  ({
    path: '/f/parcels/redelivery.md',
    rel: 'parcels/redelivery.md',
    kalka_id: 'b0f5e1de-5b3a-4c9e-9a51-2e6f0b3d7c11',
    title: 'Redelivery',
    owner: 'ewa@parcels.example',
    role: 'owner',
    self: 'ewa@parcels.example',
    rev: '',
    sections: [],
    headings: [],
    entries: [],
    problems: [],
    invariants: [],
    glossary: { state: 'none', terms: [] },
    scope: 'parcels',
    traces: [],
    refs: {},
    approval: { state: 'draft' },
    plan: {
      path: [],
      children: [],
      goals: [
        { kind: 'goal', id: 'G1', text: 'A second attempt', state: 'unclaimed', own: true },
        { kind: 'criterion', id: 'AC1', text: 'One more attempt', own: true },
      ],
    },
    team: { team: 'parcels', held: true, version: 1, releaser: 'ewa@parcels.example', can_act: true, ...team },
    ...more,
  }) as Doc

const buttons = () => [...host.querySelectorAll('button')].map((b) => b.textContent ?? '')

describe('the approval state', () => {
  it('is worded the same everywhere', () => {
    expect(approvalText({ state: 'draft' })).toBe('draft')
    expect(approvalText({ state: 'approved', version: 1 })).toBe('approved v1')
    expect(approvalText({ state: 'changed', version: 1 })).toBe('changed since approved v1')
  })
  it('lists open items as information', () => {
    expect(openItems(doc())).toEqual(['goal G1 is unclaimed', 'criterion AC1: no trace step covers it'])
  })
  it('is for the owner, and only when there is something to approve', () => {
    expect(approveRole(doc())).toBe('owner')
    expect(approveRole(doc({ approval: { state: 'approved', version: 1 } }))).toBe('')
    expect(approveRole(doc({ approval: { state: 'approved', version: 1 } }, { unreleased: true }))).toBe('owner')
    expect(approveRole(doc({ role: 'reviewer' }, { can_act: false, releaser: 'jan@parcels.example' }))).toBe('')
    expect(approveRole(doc({ role: 'reviewer' }, { can_act: false, team_owner: true }))).toBe('take-over')
    expect(approveRole(doc({ approval: undefined, scope: 'personal', team: undefined }))).toBe('')
  })
})

describe('the header', () => {
  it('names the state and offers Approve vN', async () => {
    await render(<DocHeader doc={doc({}, { unreleased: true })} reload={() => {}} />)
    expect(host.querySelector('.doc-head .badge.approval')!.textContent).toBe('draft')
    expect(buttons()).toContain('Approve v2…')
    expect(host.querySelector('[data-action="doc.approve"]')).not.toBeNull()
  })
  it('has no Approve once approved with no edits', async () => {
    await render(<DocHeader doc={doc({ approval: { state: 'approved', version: 1 } })} reload={() => {}} />)
    expect(host.querySelector('.doc-head .badge.approval')!.textContent).toBe('approved v1')
    expect(buttons().some((b) => b.startsWith('Approve'))).toBe(false)
  })
  it('says changed since approved vN', async () => {
    await render(<DocHeader doc={doc({ approval: { state: 'changed', version: 1 } }, { version: 3 })} reload={() => {}} />)
    expect(host.querySelector('.doc-head .badge.approval')!.textContent).toBe('changed since approved v1')
    expect(buttons()).toContain('Approve v3…')
  })
})

describe('the approve dialog', () => {
  const click = async (label: string) => {
    const b = [...document.querySelectorAll('button')].find((x) => x.textContent === label)
    if (!b) throw new Error('no button ' + label + ' in ' + [...document.querySelectorAll('button')].map((x) => x.textContent).join(', '))
    await act(async () => b.click())
  }
  const withIssue = (exportable: boolean) =>
    doc({ issue: { issue: 'jira:DEV-123', scheme: 'jira', payload: 'DEV-123', caps: exportable ? { meta: true, export: { attachment: true } } : { meta: true } } })

  it('lists the open items and approves; then asks to attach', async () => {
    const approve = vi.spyOn(api, 'approve').mockResolvedValue({ path: '/f/parcels/redelivery.md', version: 1, released: false, changed: true, state: 'approved v1' })
    const exp = vi.spyOn(api, 'exportDoc').mockResolvedValue({ path: '/f/parcels/redelivery.md', issue: 'jira:DEV-123', version: 1, approved: true })
    const done = vi.fn()
    await render(<ApproveDialog doc={withIssue(true)} takeOver={false} onClose={() => {}} onDone={done} />)
    expect(document.body.textContent).toContain('goal G1 is unclaimed')
    expect(document.body.textContent).toContain('they do not stop the approval')
    await click('Approve v1')
    expect(approve).toHaveBeenCalledWith('parcels/redelivery.md', '', [], false)
    expect(document.body.textContent).toContain('Attach approved v1 to DEV-123?')
    await click('Attach')
    expect(exp).toHaveBeenCalledWith('/f/parcels/redelivery.md')
    expect(document.body.textContent).toContain('Approved v1 is attached to DEV-123.')
  })
  it('keeps the approval when the export fails, and Not now attaches nothing', async () => {
    vi.spyOn(api, 'approve').mockResolvedValue({ path: '/f/parcels/redelivery.md', version: 1, released: false, changed: true, state: 'approved v1' })
    vi.spyOn(api, 'exportDoc').mockRejectedValue(new Error('the tracker is down'))
    const done = vi.fn()
    await render(<ApproveDialog doc={withIssue(true)} takeOver={false} onClose={() => {}} onDone={done} />)
    await click('Approve v1')
    await click('Attach')
    expect(document.body.textContent).toContain('The export failed: the tracker is down. The approval stands')
    await click('Not now')
    expect(done).toHaveBeenCalled()
  })
  it('does not ask without a plugin that exports', async () => {
    vi.spyOn(api, 'approve').mockResolvedValue({ path: '/f/parcels/redelivery.md', version: 1, released: false, changed: true, state: 'approved v1' })
    const exp = vi.spyOn(api, 'exportDoc')
    const done = vi.fn()
    await render(<ApproveDialog doc={withIssue(false)} takeOver={false} onClose={() => {}} onDone={done} />)
    await click('Approve v1')
    expect(done).toHaveBeenCalled()
    expect(exp).not.toHaveBeenCalled()
    expect(document.body.textContent).not.toContain('Attach approved')
  })
})
