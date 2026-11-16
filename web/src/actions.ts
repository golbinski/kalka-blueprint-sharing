// The app's workflow actions (spec 12.2). Each id is a row's action in
// internal/cli/parity.json, and a control carries it with {...act(id)}.
// parity.test.ts checks the three agree: every id here is in the table and
// on a control, and every id in the table is here.

export const ACTIONS = {
  'setup.save': 'Save the three settings (kalka setup)',
  'team.switch': 'Switch to a team or to Personal (kalka teams)',
  'team.create': 'Create team… (kalka team create)',
  'team.invite': 'Invite (kalka team invite)',
  'team.uninvite': 'Cancel an open invite (kalka team uninvite)',
  'team.join': 'Join with link or token…, Join (kalka join)',
  'team.remove': 'Remove a member (kalka team remove)',
  'team.owner': 'Make owner, Remove as owner (kalka team add-owner, remove-owner)',
  'team.sync': 'Sync now (kalka team sync)',
  'doc.publish': 'Publish to team… (kalka publish)',
  'doc.release': 'Release (kalka release)',
  'doc.handoff': 'Hand off… (kalka handoff)',
  'doc.reassign': 'Reassign… (kalka reassign)',
  'doc.copy': 'Copy to team… (kalka copy)',
  'doc.move': 'Move to team… (kalka move)',
  'doc.delete': 'Delete for the team (kalka delete)',
  'transfer.accept': 'Accept a transfer (kalka transfer accept)',
  'transfer.decline': 'Decline a transfer (kalka transfer decline)',
  'transfer.withdraw': 'Withdraw your transfer (kalka transfer withdraw)',
  'entry.add': 'Comment, deviation, reply (kalka comment)',
  'seed.create': 'Create seed (kalka seed)',
  'seed.backlink': 'Add the backlink (kalka seed backlink)',
  'seed.promote': 'Promote (kalka promote)',
  'sync.pause': 'Pause and resume sync (kalka pause, resume)',
  'settings.save': 'Notifications, keep running when closed (kalka settings)',
  'settings.autostart': 'Start at login (kalka autostart)',
  'reference.export': 'Blueprint reference: save a copy (kalka reference export)',
} as const

export type ActionId = keyof typeof ACTIONS

/** act marks a control as the app's side of a command. */
export function act(id: ActionId): { 'data-action': ActionId } {
  return { 'data-action': id }
}
