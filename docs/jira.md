# Jira

kalka links a blueprint to the Jira issue it addresses, shows that issue's
title, status and parents beside the document, groups the Blueprints list by
them, and exports the final version to the issue. It does this through
`kalka-plugin-jira`, which ships beside `kalka` and `kalka-plugin-gh` in every
archive, installer and app bundle.

What Jira says about an issue is fetched by your own kalka, with your own
credentials, and kept on your machine only. It is never written into the
blueprint and never sent to your teammates: each of them sees the issue as
their own Jira account does, or only its key if they have no plugin set up.
Nothing in Jira comes back into a blueprint.

## Name the issue in the blueprint

Add one key to the frontmatter:

```markdown
---
kalka_issue: jira:DEV-123
---
# Redelivery after a failed attempt
```

One issue per blueprint. `kalka check` warns about a value that is not
`<scheme>:<key>` (`F020`); it needs no plugin and no network for that.
Links in the text, such as `[the story](jira:DEV-140)`, resolve too.

## Set it up

In kalka: **Settings → Plugins → jira → Set up…**. The form asks for:

| | Jira Cloud | Jira Data Center or Server |
|---|---|---|
| Site | `https://example.atlassian.net` | your server's address, `https` |
| Account email | your Atlassian email | — |
| Token | an API token: id.atlassian.com → Security → API tokens | a personal access token: your profile → Personal Access Tokens |

*Save* checks the token against the site before anything is stored; if Jira
refuses it, the form stays open with what you typed and says which field to
fix. The token field is always empty when the form opens: a stored token is
never shown again, only that one is stored. Leave it empty to keep it, unless
you change the site or switch between Cloud and Data Center, which needs the
token again. *Check* asks Jira whether the stored credentials still work;
*Log out…* removes them.

The same from the command line, without kalka running:

```
kalka plugin set jira site=https://example.atlassian.net email=ewa@example.org --secret-stdin token
```

On a terminal it asks for the token without showing it; in a script, pass it
on standard input (`printf '%s\n' "$TOKEN" | kalka plugin set jira … --secret-stdin token`).
A token is never taken as an argument. For Data Center add `flavor=datacenter`
and leave out the email. `kalka plugin set jira` with nothing else, on a
terminal, asks for every field. `kalka plugin settings jira --check` shows the
site and whether Jira accepts the credentials; `kalka plugin clear jira`
removes them. If kalka is running, it is told and drops what it fetched with
the old credentials.

### Where the credentials are

- The token goes into the system's credential store: the macOS Keychain, the
  Windows Credential Manager, or on Linux the Secret Service through
  `secret-tool` (from `libsecret-tools`).
- Where there is none, it goes into the plugin's `config.json`, readable by
  you only (mode 0600).
- `config.json` holds the site, the flavor and your email. It is in
  `kalka-plugin-jira` in your configuration folder (`~/Library/Application
  Support` on macOS, `%AppData%` on Windows, `~/.config` on Linux), or in
  `$KALKA_PLUGIN_JIRA_DIR`.
- Settings, and `kalka plugin settings jira`, say which of the two holds
  your token.
- Credentials are never in a blueprint and never in kalka's own
  configuration: kalka only passes what you enter to the plugin. One site per
  user.
- When you log out or change the site, what kalka fetched from Jira with the
  old credentials is deleted, on disk too.

## What you see

- **The document bar**: the issue's key, title and status, linked to Jira,
  and its parents (the epic, and what is above it) as a breadcrumb. When Jira
  cannot be reached, the last answer is shown "as of" its time. Answers are
  kept for 15 minutes.
- **Blueprints**: *Group by* parent issue, at the level you pick (Epic, or a
  level above it), or by status (to do, in progress, done). Blueprints with no
  parent, whose issue you cannot read, or with no issue come last. The choice
  is remembered per team in your browser.

Levels above Epic come from your site's issue hierarchy (Jira Cloud plans,
or Premium hierarchy levels). On Data Center, only sub-tasks have a parent.

## Export the final version

When a version is released, its owner or a team owner can export it:
*Export…* in the document bar, or

```
kalka export redelivery.md
```

The plugin attaches the whole file to the issue (`redelivery-v3.md`) and
writes a comment with the released plan, the deviations the release took up,
and a line saying it came from kalka. kalka then adds an `exported` entry to
the review log, which your teammates receive like any comment.

Exporting again, after a later release, replaces that attachment and updates
the same comment; a comment someone deleted in Jira is written anew. A failed
export changes nothing in the blueprint.

Only the released version goes: with edits not yet released, release first.

## Archive

Once the work lives in Jira, *Archive* (offered after the export, under More,
or `kalka archive <file>`) hides the blueprint from the lists and the rail. It
still replicates and opens from links; *Show archived* lists it again, and
*Unarchive* (`kalka archive --undo <file>`) brings it back. Archiving is a
change to the plan, so its owner makes it, released as the next version.

## Limits and errors

- Keys must look like `DEV-123` (upper-case project key). They go to Jira as
  API parameters only.
- Jira's rate limits are waited out (it says how long) for up to a few
  seconds, then reported; the cached answer stays on screen.
- "Jira rejected the credentials": the token expired or was revoked; set a
  new one in Settings → Plugins, or with `kalka plugin set jira --secret-stdin token`.
- "not connected to Jira": the plugin is not set up yet; Settings → Plugins
  says so too, and `kalka doctor` lists it as not set up, which is fine if you
  do not use Jira.
- "not found or no permission": the issue does not exist, or your account
  cannot see it.
