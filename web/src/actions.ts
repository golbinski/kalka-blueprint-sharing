// The app's workflow actions (spec 12.2). Each id is a row's action in
// internal/cli/parity.json, and a control carries it with {...act(id)}.
// parity.test.ts checks the three agree: every id here is in the table and
// on a control, and every id in the table is here.

export const ACTIONS = {
  'setup.save': 'Save the three settings (kalka setup)',
  'team.switch': 'Switch to a team or to Personal (kalka teams)',
  'team.create': 'Create team… (kalka team create)',
  'team.invite': 'Create invite (kalka team invite)',
  'team.uninvite': 'Cancel an open invite (kalka team uninvite)',
  'team.join': 'Join with an invite…, Join, Cancel this join (kalka join, join --list, join --forget)',
  'team.remove': 'Remove from team… (kalka team remove)',
  'team.owner': 'Make team owner…, Remove as team owner… (kalka team add-owner, remove-owner)',
  'team.sync': 'Sync now (kalka team sync)',
  'doc.publish': 'Publish to team… (kalka publish)',
  'doc.release': 'Release vN, Take over and release… (kalka release)',
  'doc.handoff': 'Change owner… as the owner (kalka handoff)',
  'doc.reassign': 'Change owner… as team owner, Choose owner…, Give their blueprints to someone… (kalka reassign)',
  'doc.copy': 'Copy to another team… (kalka copy)',
  'doc.move': 'Move to another team… (kalka move)',
  'doc.delete': 'Delete for the team…, Take over and delete… (kalka delete)',
  'doc.export': 'Export to the issue… (kalka export)',
  'doc.archive': 'Archive, Unarchive (kalka archive)',
  'doc.close': 'Close as delivered, Reopen (kalka close)',
  'transfer.accept': 'Accept a transfer (kalka transfer accept)',
  'transfer.decline': 'Decline a transfer (kalka transfer decline)',
  'transfer.withdraw': 'Withdraw your transfer (kalka transfer withdraw)',
  'entry.add': 'Comment, deviation, reply (kalka comment)',
  'seed.create': 'Split off a seed… (kalka seed)',
  'seed.backlink': 'Link it from its parent (kalka seed backlink)',
  'seed.promote': 'Turn into a blueprint (kalka promote)',
  'sync.pause': 'Pause and resume sync (kalka pause, resume)',
  'settings.save': 'Notifications, keep running when closed (kalka settings)',
  'settings.server': 'Change server… (kalka settings --rendezvous)',
  'update.install': 'Restart to update, Download and restart (kalka update)',
  'settings.autostart': 'Start at login (kalka autostart)',
  'samples.add': 'Add sample blueprints (kalka samples)',
  'reference.export': 'Blueprint writing guide for agents: save a copy (kalka reference export)',
} as const

export type ActionId = keyof typeof ACTIONS

/** act marks a control as the app's side of a command. */
export function act(id: ActionId): { 'data-action': ActionId } {
  return { 'data-action': id }
}
