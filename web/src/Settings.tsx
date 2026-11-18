import { useCallback, useEffect, useState } from "react";
import { act } from "./actions";
import { api, referenceURL, type About, type DoctorReport, type Settings, type Status, type Summary } from "./api";
import { localTime } from "./util";
import { AddSamples, OpenFolder } from "./Views";
import { inDesktopWindow } from "./shell";

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
        </dd>
      </dl>
      <p className="muted small">
        To change these, run <code>kalka setup</code> in a terminal. A new
        email or server means teammates have to invite you again.
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
