# Show when a session uses the relay

A first sketch, with no frontmatter: kalka lists it in Personal until it is published.

## Idea

When two members cannot connect directly, their sessions go through the rendezvous's TURN relay. That works, but it is slower, and on a self-hosted rendezvous it uses the host's bandwidth. Today nothing shows it: the Team page says only "synced".

## Questions

- Is the relay worth showing per member, or only as a count on the Team page?
- Should `kalka status --json` report it, so a script can warn when most sessions are relayed?
- Can the desktop app tell a relayed session apart without asking the WebRTC stack for statistics on every round?
