import { useCallback, useEffect, useState } from "react";
import { act } from "./actions";
import { ApiError, api, referenceURL, type About, type DoctorReport, type Settings, type Status, type Summary, type UpdateStatus } from "./api";
import { localTime } from "./util";
import { AddSamples, OpenFolder } from "./Views";
import { inDesktopWindow } from "./shell";
import { PluginsSection } from "./Plugins";

// SettingsPage is the Settings view (spec 11, 22.2): the daemon keeps the
// values in state.yaml; start at login is the system's own entry.
export function SettingsPage({ tick, status }: { tick: number; status: Status }) {
  const [s, setS] = useState<Settings | null>(null);
  const [sum, setSum] = useState<Summary | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [about, setAbout] = useState<About | null>(null);
  const [copied, setCopied] = useState("");
  const load = useCallback(() => {
    api.settings().then(setS, (e: Error) => setErr(e.message));
    api.summary().then(setSum, () => {});
    api.about().then(setAbout, () => {});
  }, []);
  useEffect(() => {
    load();
  }, [load, tick]);
  const change = async (patch: Settings) => {
    setBusy(true);
    setErr("");
    try {
      setS(await api.applySettings(patch));
      api.summary().then(setSum, () => {});
    } catch (e) {
      setErr((e as Error).message);
    }
    setBusy(false);
  };
  if (!s) return <div className="page muted">{err || "Loading…"}</div>;
  return (
    <div className="page narrow">
      <h1>Settings</h1>
      {sum && (
        <p className="small">
          {sum.paused
            ? "Paused"
            : sum.online
              ? "Online"
              : "Offline" + (sum.error ? `: ${sum.error}` : "")}
          {" · "}
          {sum.peers_online} of {sum.peers_known} members reachable
          {" · "}
          last sync{" "}
          {sum.last_sync ? localTime(sum.last_sync) : "none since start"}
          {sum.waiting.total > 0 && <> · {sum.waiting.total} waiting for you</>}
          {sum.pushes > 0 && (
            <>
              {" "}
              · {sum.pushes} team push{sum.pushes > 1 ? "es" : ""} queued
            </>
          )}
        </p>
      )}
      {err && <div className="error-box">{err}</div>}
      <h2>You</h2>
      <dl className="facts">
        <dt>Email</dt>
        <dd>
          {status.email}{" "}
          <span className="muted small">how teammates find you; not an account</span>
        </dd>
        <dt>Folder</dt>
        <dd>
          <span className="small">{status.folder}</span> <OpenFolder />
        </dd>
        <dt>Samples</dt>
        <dd className="row center">
          <AddSamples />
          <span className="muted small">blueprints to try kalka with, in Personal</span>
        </dd>
        <dt>Server</dt>
        <dd className="small">
          {status.rendezvous === "peerjs-cloud"
            ? "The public demo server"
            : status.rendezvous}{" "}
          <span className="muted">(rendezvous)</span>
          <ChangeServer current={status.rendezvous} />
        </dd>
      </dl>
      <p className="muted small">
        The email and the folder are changed with <code>kalka setup</code> in a
        terminal. A new email means teammates have to invite you again.
      </p>
      <CheckSetup />
      <h2>Sync and notifications</h2>
      <div className="settings">
        <label className="setting">
          <input
            type="checkbox"
            checked={!!s.paused}
            {...act("sync.pause")}
            disabled={busy}
            onChange={(e) => change({ paused: e.target.checked })}
          />
          <span>
            <b>Pause sync</b>
            <span className="muted small">
              kalka goes offline: no syncs and no joins. Your
              own comments, seeds and releases are kept and sent after you
              resume. The pause lasts across restarts.
            </span>
          </span>
        </label>
        <label className="setting">
          <input
            type="checkbox"
            checked={!!s.notifications}
            {...act("settings.save")}
            disabled={busy}
            onChange={(e) => change({ notifications: e.target.checked })}
          />
          <span>
            <b>Notifications</b>
            <span className="muted small">
              New comments on your blueprints, joins, newer domain versions.
              Everything also stays listed under Needs you.
            </span>
          </span>
        </label>
        <label className="setting">
          <input
            type="checkbox"
            checked={!!s.start_at_login}
            {...act("settings.autostart")}
            disabled={busy || !s.start_at_login_available}
            onChange={(e) => change({ start_at_login: e.target.checked })}
          />
          <span>
            <b>Start at login</b>
            <span className="muted small">
              {s.start_at_login_available
                ? "kalka syncs only while teammates are online at the same time, so it runs in the background from login."
                : "This kalka cannot change its login entry; use `kalka autostart on` in a terminal."}
              {s.start_at_login_error && <> ({s.start_at_login_error})</>}
            </span>
          </span>
        </label>
        {inDesktopWindow() && (
          <label className="setting">
            <input
              type="checkbox"
              checked={!!s.keep_running_on_close}
              {...act("settings.save")}
              disabled={busy}
              onChange={(e) =>
                change({ keep_running_on_close: e.target.checked })
              }
            />
            <span>
              <b>Keep running when the window is closed</b>
              <span className="muted small">
                Closing this window hides it, and kalka keeps syncing. Off:
                closing the window quits kalka.
              </span>
            </span>
          </label>
        )}
      </div>
      <h2>Updates</h2>
      <UpdatesSection s={s} busy={busy} change={change} tick={tick} />
      <h2>Plugins</h2>
      <PluginsSection tick={tick} />
      <h2>Blueprint writing guide for agents</h2>
      <p className="small">
        How a blueprint is written, for agents: drop it in as a skill or an
        instruction file{about ? ` (${about.reference})` : ""}.{" "}
        <a href={referenceURL} download {...act("reference.export")}>
          Save a copy
        </a>
        {" · "}
        <button
          type="button"
          className="link"
          onClick={() =>
            api
              .reference()
              .then((t) => navigator.clipboard?.writeText(t))
              .then(
                () => setCopied("Copied."),
                () => setCopied("Could not copy; use kalka reference export."),
              )
          }
        >
          Copy
        </button>{" "}
        <span className="muted">{copied}</span>
      </p>
      {about && (
        <details className="small">
          <summary>kalka {about.version}</summary>
          <p className="muted">
            Wire protocol {about.wire_protocol} · plugin protocol{" "}
            {about.plugin_protocol} · reference {about.reference}
            {status.fingerprint && <> · your key {status.fingerprint}</>}
          </p>
        </details>
      )}
    </div>
  );
}

// ChangeServer switches the rendezvous server while kalka runs (spec 11.1).
// A server that does not answer is refused until the person insists.
function ChangeServer({ current }: { current: string }) {
  const [open, setOpen] = useState(false);
  const [cloud, setCloud] = useState(current === "peerjs-cloud");
  const [url, setUrl] = useState(current === "peerjs-cloud" ? "" : current);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [silent, setSilent] = useState(false);
  const value = cloud ? "peerjs-cloud" : url.trim();
  const apply = async (force: boolean) => {
    setBusy(true);
    setErr("");
    setSilent(false);
    try {
      await api.setRendezvous(value, force);
      setOpen(false);
    } catch (e) {
      setErr((e as Error).message);
      setSilent(e instanceof ApiError && e.kind === "rendezvous-silent");
    }
    setBusy(false);
  };
  if (!open) {
    return (
      <>
        {" "}
        <button type="button" {...act("settings.server")} onClick={() => setOpen(true)}>
          Change…
        </button>
      </>
    );
  }
  return (
    <div className="check-setup">
      <label className="setting">
        <input type="radio" name="server" checked={cloud} onChange={() => setCloud(true)} />
        <span>
          <b>The public demo server (peerjs-cloud)</b>
        </span>
      </label>
      <label className="setting">
        <input type="radio" name="server" checked={!cloud} onChange={() => setCloud(false)} />
        <span>
          <b>A kalka server</b>
          <input
            type="text"
            value={url}
            placeholder="wss://kalka.example.org"
            disabled={cloud}
            onChange={(e) => setUrl(e.target.value)}
          />
        </span>
      </label>
      <ul className="small">
        <li>
          Teammates reach you only when they use the same server, so the team
          moves together: each member changes it here.
        </li>
        <li>
          Invites you created that are still open name the old server and stop
          working; create new ones.
        </li>
        <li>
          A join still waiting for someone on the old server will not complete
          until they move too.
        </li>
      </ul>
      {err && <div className="error-box">{err}</div>}
      <div className="row center">
        <button type="button" disabled={busy || !value} onClick={() => apply(false)}>
          {busy ? "Switching…" : "Switch server"}
        </button>
        {silent && (
          <button type="button" disabled={busy} onClick={() => apply(true)}>
            Switch anyway
          </button>
        )}
        <button type="button" disabled={busy} onClick={() => setOpen(false)}>
          Cancel
        </button>
      </div>
    </div>
  );
}

const STATE: Record<string, string> = { ok: "ok", warn: "check", fail: "problem", skip: "not checked" };

// CheckSetup runs kalka doctor's checks (spec 12.5) and lists them, each
// with what to do about it.
function CheckSetup() {
  const [r, setR] = useState<DoctorReport | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const run = async () => {
    setBusy(true);
    setErr("");
    try {
      setR(await api.doctor());
    } catch (e) {
      setErr((e as Error).message);
    }
    setBusy(false);
  };
  const bad = r ? r.summary.warn + r.summary.fail : 0;
  return (
    <div className="check-setup">
      <div className="row center">
        <button type="button" onClick={run} disabled={busy}>
          {busy ? "Checking…" : r ? "Check again" : "Check setup"}
        </button>
        <span className="muted small">
          {r
            ? bad
              ? `${bad} to look at, as kalka doctor reports them`
              : "Everything works."
            : "The key, the daemon, the server, the clock, the folder and links, as kalka doctor checks them."}
        </span>
      </div>
      {err && <div className="error-box">{err}</div>}
      {r && (
        <ul className="plain doctor-list">
          {r.results.map((x) => (
            <li key={x.check} className={"doctor-" + x.state}>
              <span className="doctor-state">{STATE[x.state] ?? x.state}</span>{" "}
              <span className="mono small muted">{x.check}</span> {x.detail}
              {x.fix && <div className="small doctor-fix">{x.fix}</div>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// UpdatesSection is the update check and install (spec 24): the checkbox
// picks whether a compatible update downloads on its own; nothing restarts
// without a click.
function UpdatesSection({ s, busy, change, tick }: { s: Settings; busy: boolean; change: (p: Settings) => void; tick: number }) {
  const [u, setU] = useState<UpdateStatus | null>(null);
  const [working, setWorking] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    api.update().then(setU, () => {});
  }, [tick]);
  const run = async (f: () => Promise<unknown>) => {
    setWorking(true);
    setErr("");
    try {
      await f();
    } catch (e) {
      setErr((e as Error).message);
    }
    api.update().then(setU, () => {});
    setWorking(false);
  };
  if (!u) return null;
  const notes = u.notes_url && (
    <>
      {" "}
      <a href={u.notes_url} target="_blank" rel="noreferrer">
        Release notes
      </a>
    </>
  );
  let line: React.ReactNode;
  if (!u.checks) line = "This is a development build; it does not check for updates.";
  else if (u.state === "ready") line = <>kalka {u.latest} is ready.{notes}</>;
  else if (u.state === "downloading") line = <>Downloading kalka {u.latest}…</>;
  else if (u.state === "available")
    line = (
      <>
        kalka {u.latest} is available.{notes}
        {!u.can_install && " This installation does not update itself; download it from the release page."}
      </>
    );
  else if (u.state === "error") line = <>The last check failed: {u.error}</>;
  else line = <>kalka {u.current} is up to date.</>;
  return (
    <div className="settings">
      <label className="setting">
        <input
          type="checkbox"
          checked={!!s.auto_update}
          {...act("settings.save")}
          disabled={busy}
          onChange={(e) => change({ auto_update: e.target.checked })}
        />
        <span>
          <b>Install updates automatically</b>
          <span className="muted small">
            A compatible update (same major version; before 1.0, the same minor
            version) is downloaded and verified in the background. kalka restarts
            only when you click Restart to update. Off, or for a new major
            version, it shows that an update is available and downloads it when
            you ask.
          </span>
        </span>
      </label>
      <p className="small">{line}</p>
      {err && <div className="error-box">{err}</div>}
      <p className="row center">
        {u.can_install && (u.state === "ready" || u.state === "available") && (
          <button
            type="button"
            {...act("update.install")}
            disabled={working}
            onClick={() => run(() => api.installUpdate())}
          >
            {u.state === "ready" ? "Restart to update" : "Download and restart"}
          </button>
        )}
        {u.checks && (
          <button type="button" disabled={working || u.state === "downloading"} onClick={() => run(() => api.checkUpdate())}>
            Check for updates
          </button>
        )}
        {u.checks && u.checked_at && <span className="muted small">Last checked {localTime(u.checked_at)}</span>}
      </p>
    </div>
  );
}
