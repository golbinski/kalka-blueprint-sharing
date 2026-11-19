// The document header's state (spec 11.1): one sentence of who owns the
// blueprint and which version the team has, a second line with what
// follows from it, and at most one primary action. Problems (two owners,
// no owner, a move under way) take the sentence's place instead of adding
// a strip.
import type { Doc } from './api'
import { shortName } from './scope'

export type Primary = 'release' | 'publish' | 'choose' | 'domain-release' | ''

export interface DocState {
  tone: '' | 'warn' | 'danger'
  head: string
  sub: string
  primary: Primary
}

const who = (doc: Doc, email?: string) => (!email ? 'someone' : email.toLowerCase() === doc.self.toLowerCase() ? 'you' : shortName(email))

export function docState(doc: Doc): DocState {
  const t = doc.team
  const dm = doc.domain
  if (doc.transfer?.moving_to) {
    return { tone: 'warn', head: `Being moved to ${doc.transfer.moving_to}`, sub: 'Comments added now will not travel with it.', primary: '' }
  }
  if (!t) {
    // A note never sent has no kalka_id and the local role: it is yours too.
    if (doc.role === 'owner' || doc.role === 'local') {
      return {
        tone: '',
        head: doc.scope === 'personal' ? 'Yours · only on this computer' : 'Yours · not shared yet',
        sub: 'Publish it to a team for your teammates to comment on it.',
        primary: doc.problems.some((p) => p.blocks) ? '' : 'publish',
      }
    }
    return { tone: '', head: 'Not shared yet', sub: 'Only in your copy; nobody else sees it.', primary: '' }
  }
  if (!t.held) {
    return { tone: '', head: `Published to ${t.team}`, sub: 'You are not a member of that team on this computer, so it does not sync from here.', primary: '' }
  }
  if (dm?.viewing) {
    return { tone: '', head: `Version ${dm.viewing} of the ${dm.name} domain`, sub: 'As kept on this computer. Read only.', primary: '' }
  }
  const owner = who(doc, t.releaser)
  if (t.disputed) {
    return {
      tone: 'warn',
      head: `Two owners: two people released version ${t.version}`,
      sub: t.team_owner ? 'Nobody can release it until you, as a team owner, choose its owner.' : 'Nobody can release it until a team owner chooses its owner.',
      primary: t.team_owner ? 'choose' : '',
    }
  }
  if (t.owner_not_member) {
    return {
      tone: 'danger',
      head: `No owner: ${owner} is no longer a member`,
      sub: `It stays at version ${t.version} until ${t.team_owner ? 'you, as a team owner, choose' : 'a team owner chooses'} a new owner.`,
      primary: t.team_owner ? 'choose' : '',
    }
  }
  const pending = t.reassigned_to ? ` Changing owner to ${who(doc, t.reassigned_to)}: it completes when their kalka is online.` : ''
  const thing = dm ? `the ${dm.name} domain` : 'this'
  if (t.version === 0) {
    return { tone: '', head: `${dm ? `The ${dm.name} domain` : 'Published'} · a draft in ${t.team}`, sub: 'Not released yet.' + pending, primary: t.can_act && dm ? 'domain-release' : '' }
  }
  if (t.can_act) {
    return {
      tone: '',
      head: `You own ${thing} · version ${t.version} is with ${t.team}`,
      sub: (t.unreleased
        ? `You have edits your teammates cannot see yet. Releasing sends them as version ${t.version + 1}.`
        : 'Your teammates see what you see.') + pending,
      primary: t.unreleased ? (dm ? 'domain-release' : 'release') : '',
    }
  }
  return {
    tone: '',
    head: `${owner === 'someone' ? 'Someone' : owner} owns ${thing} · you are reviewing version ${t.version}`,
    sub: (t.unreleased
      ? `Your copy has edits that are not released; only ${owner} releases versions.`
      : `Your comments reach everyone in ${t.team}.`) + pending,
    primary: '',
  }
}
