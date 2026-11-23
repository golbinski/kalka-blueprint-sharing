import { useEffect, useId, useRef, useState } from "react";
import { act } from "./actions";
import { ApiError, api, type PluginField, type PluginList, type PluginListed, type PluginSettings, type PluginStatus } from "./api";

// PluginsSection lists the installed plugins in Settings (spec 10.6, 11.1):
// where each was found, what it does, its state, and its settings form.
// kalka keeps none of the values: each plugin stores its own, and a stored
// secret never comes back to the page.
export function PluginsSection({ tick }: { tick: number }) {
  const [list, setList] = useState<PluginList | null>(null);
  const [err, setErr] = useState("");
  // One form is open at a time.
  const [open, setOpen] = useState<string | null>(null);
  useEffect(() => {
    api.plugins().then(
      (l) => {
        setList(l);
        setErr("");
      },
      (e: Error) => setErr(e.message),
    );
  }, [tick]);
  if (!list) return <p className="muted small">{err || "Loading…"}</p>;
  if (list.plugins.length === 0) {
    return (
      <p className="muted small">
        No plugins found. kalka looks in {list.dirs.config}, beside itself
        {list.dirs.bundled ? ` (${list.dirs.bundled})` : ""} and on the PATH it
        was started with.
      </p>
    );
  }
  return (
    <div className="plugins">
      {list.plugins.map((p) => (
        <PluginBlock key={p.scheme} p={p} tick={tick} open={open === p.scheme} setOpen={(o) => setOpen(o ? p.scheme : null)} />
      ))}
    </div>
  );
}

function sourceWords(p: PluginListed): string {
  switch (p.source) {
    case "bundled":
      return "shipped with kalka";
    case "config":
      return "from your plugins folder: " + p.file;
  }
  return "found on PATH: " + p.file;
}

function capWords(p: PluginListed): string {
  const c = p.capabilities;
  const w: string[] = [];
  if (c?.resolve) w.push("References");
  if (c?.issue?.meta) w.push("Issue details");
  if (c?.issue?.export && (c.issue.export.attachment || c.issue.export.comment)) w.push("Export");
  return w.join(", ");
}

const STATE: Record<PluginStatus["state"], string> = { ready: "Ready", "needs-setup": "Not set up", error: "Problem" };

function stateWord(s: PluginStatus): string {
  if (s.state === "ready" && !s.checked) return "Ready (not checked)";
  return STATE[s.state] ?? "Problem";
}

function PluginBlock({ p, tick, open, setOpen }: { p: PluginListed; tick: number; open: boolean; setOpen: (o: boolean) => void }) {
  const headId = useId();
  const [ps, setPs] = useState<PluginSettings | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [clearErr, setClearErr] = useState("");
  const opener = useRef<HTMLButtonElement>(null);
  const hasSettings = !p.problem && !!p.capabilities?.settings;
  const load = (check: boolean) => {
    setBusy(true);
    setErr("");
    api.pluginSettings(p.scheme, check).then(
      (s) => {
        setPs(s);
        setBusy(false);
      },
      (e: Error) => {
        setErr(e.message);
        setBusy(false);
      },
    );
  };
  useEffect(() => {
    if (hasSettings) load(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [p.scheme, hasSettings, tick]);
  // Closing the form returns focus to the button that opened it, once
  // that is shown again.
  const wasOpen = useRef(false);
  useEffect(() => {
    if (wasOpen.current && !open) opener.current?.focus();
    wasOpen.current = open;
  }, [open]);
  const close = () => setOpen(false);
  const clear = async () => {
    setBusy(true);
    setClearErr("");
    try {
      setPs(await api.clearPluginSettings(p.scheme));
      setConfirm(false);
    } catch (e) {
      setClearErr((e as Error).message);
    }
    setBusy(false);
  };
  const title = p.name ? `${p.name} ${p.version ?? ""}`.trim() : "";
  return (
    <section className="plugin" aria-labelledby={headId}>
      <h3 id={headId} className="plugin-head">
        <code>{p.scheme}:</code> {title}
        {title && " · "}
        <span className="muted small">{sourceWords(p)}</span>
      </h3>
      {p.shadowed.map((f) => (
        <div key={f} className="small muted">
          Used instead of {f}.
        </div>
      ))}
      {p.problem ? (
        <div className="small" role="status">
          <b>Problem</b> · {p.problem}
        </div>
      ) : (
        <>
          {capWords(p) && <div className="small">{capWords(p)}</div>}
          {hasSettings && (
            <div className="small plugin-state" role="status">
              {err ? (
                <>
                  <b>Problem</b> · {err}
                </>
              ) : !ps || (busy && !open && !confirm) ? (
                "Checking…"
              ) : (
                <>
                  <b>{stateWord(ps.status)}</b> · {ps.status.message}
                  {ps.status.detail && <div className="muted">{ps.status.detail}</div>}
                </>
              )}
            </div>
          )}
          {ps?.hint && <Hint hint={ps.hint} />}
          {ps?.incomplete && <div className="small muted">This plugin has settings this kalka cannot show. Update kalka.</div>}
          {hasSettings && !open && !confirm && (
            <div className="row center">
              {ps && ps.can.apply && ps.fields.length > 0 && (
                <button type="button" ref={opener} onClick={() => setOpen(true)} disabled={busy}>
                  {ps.status.state === "needs-setup" ? "Set up…" : "Change…"}
                </button>
              )}
              <button type="button" onClick={() => load(true)} disabled={busy}>
                Check
              </button>
              {ps && ps.can.clear && ps.status.state !== "needs-setup" && (
                <button type="button" onClick={() => setConfirm(true)} disabled={busy}>
                  Log out…
                </button>
              )}
            </div>
          )}
          {confirm && (
            <div className="check-setup">
              <p className="small">
                Remove the stored settings of {p.name || p.scheme}? Links and issue details from it stop until you set it up again.
              </p>
              {clearErr && (
                <div className="error-box" role="alert">
                  {clearErr}
                </div>
              )}
              <div className="row center">
                <button type="button" {...act("plugin.clear")} onClick={clear} disabled={busy}>
                  {busy ? "Logging out…" : "Log out"}
                </button>
                <button type="button" onClick={() => setConfirm(false)} disabled={busy}>
                  Cancel
                </button>
              </div>
            </div>
          )}
          {open && ps && (
            <PluginForm
              name={p.name || p.scheme}
              ps={ps}
              onSaved={(s) => {
                setPs(s);
                close();
              }}
              onCancel={close}
            />
          )}
        </>
      )}
    </section>
  );
}

function Hint({ hint }: { hint: NonNullable<PluginSettings["hint"]> }) {
  const [copied, setCopied] = useState("");
  return (
    <div className="small plugin-hint">
      {hint.text}
      {hint.command && (
        <>
          {" "}
          <code>{hint.command}</code>{" "}
          <button
            type="button"
            className="link"
            onClick={() =>
              (navigator.clipboard?.writeText(hint.command!) ?? Promise.reject()).then(
                () => setCopied("Copied."),
                () => setCopied("Could not copy."),
              )
            }
          >
            Copy
          </button>{" "}
          <span className="muted">{copied}</span>
        </>
      )}
      {hint.url && (
        <>
          {" "}
          <a href={hint.url} target="_blank" rel="noreferrer">
            {hint.url}
          </a>
        </>
      )}
    </div>
  );
}

type Values = Record<string, string | boolean>;

// visible applies a field's when rule to the values being entered.
export function visible(f: PluginField, values: Values): boolean {
  if (!f.when) return true;
  return f.when.is.includes(values[f.when.field] as string | boolean);
}

function initial(ps: PluginSettings): Values {
  const v: Values = {};
  for (const f of ps.fields) {
    if (f.kind === "secret") continue;
    v[f.id] = f.value ?? (f.kind === "bool" ? false : "");
  }
  return v;
}

// PluginForm edits a plugin's settings. What is typed into a secret lives
// in this component's state only, is sent once under secrets, and goes
// when the form closes.
function PluginForm({ name, ps, onSaved, onCancel }: { name: string; ps: PluginSettings; onSaved: (s: PluginSettings) => void; onCancel: () => void }) {
  const [values, setValues] = useState<Values>(() => initial(ps));
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [fieldErrs, setFieldErrs] = useState<Record<string, string>>({});
  const formRef = useRef<HTMLFieldSetElement>(null);
  const errRef = useRef<HTMLDivElement>(null);
  const base = useId();
  const orig = initial(ps);
  useEffect(() => {
    formRef.current?.querySelector<HTMLElement>("input, select")?.focus();
  }, []);
  const shown = ps.fields.filter((f) => visible(f, values));
  const filled = (f: PluginField) =>
    f.kind === "secret" ? !!secrets[f.id]?.trim() || !!f.set : f.kind === "bool" || String(values[f.id] ?? "").trim() !== "";
  const ready = shown.every((f) => !f.required || filled(f));
  const save = async () => {
    setBusy(true);
    setErr("");
    setFieldErrs({});
    const send: Values = {};
    const sec: Record<string, string> = {};
    for (const f of shown) {
      if (f.kind === "secret") {
        if (secrets[f.id]?.trim()) sec[f.id] = secrets[f.id];
      } else if (values[f.id] !== orig[f.id]) {
        send[f.id] = values[f.id];
      }
    }
    try {
      const s = await api.applyPluginSettings(ps.scheme, send, sec);
      setSecrets({});
      onSaved(s);
      return;
    } catch (e) {
      const fe = (e instanceof ApiError && e.fields) || {};
      setErr((e as Error).message);
      setFieldErrs(fe);
      setBusy(false);
      // Focus the first field with an error, or the error.
      setTimeout(() => {
        const first = ps.fields.find((f) => fe[f.id]);
        const el = first ? document.getElementById(base + first.id) : null;
        (el ?? errRef.current)?.focus();
      });
    }
  };
  return (
    <form
      className="check-setup plugin-form"
      onSubmit={(e) => {
        e.preventDefault();
        if (ready && !busy) save();
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape" && !busy) {
          e.preventDefault();
          setSecrets({});
          onCancel();
        }
      }}
    >
      <fieldset ref={formRef} disabled={busy}>
        <legend>{name}</legend>
        {shown.map((f) => (
          <FieldControl
            key={f.id}
            id={base + f.id}
            f={f}
            value={f.kind === "secret" ? secrets[f.id] ?? "" : values[f.id]}
            error={fieldErrs[f.id]}
            onChange={(v) => (f.kind === "secret" ? setSecrets((s) => ({ ...s, [f.id]: v as string })) : setValues((s) => ({ ...s, [f.id]: v })))}
          />
        ))}
        {err && (
          <div className="error-box" role="alert" tabIndex={-1} ref={errRef}>
            {err}
          </div>
        )}
        <div className="row center">
          <button type="submit" {...act("plugin.save")} disabled={!ready || busy}>
            {busy ? "Checking…" : "Save"}
          </button>
          <button
            type="button"
            onClick={() => {
              setSecrets({});
              onCancel();
            }}
          >
            Cancel
          </button>
        </div>
      </fieldset>
    </form>
  );
}

function FieldControl({ id, f, value, error, onChange }: { id: string; f: PluginField; value: string | boolean | undefined; error?: string; onChange: (v: string | boolean) => void }) {
  const helpId = id + "-help";
  const errId = id + "-err";
  const described = [f.help ? helpId : "", error ? errId : ""].filter(Boolean).join(" ") || undefined;
  const common = { id, "aria-describedby": described, "aria-invalid": error ? true : undefined, required: f.required };
  let control;
  switch (f.kind) {
    case "bool":
      return (
        <div className="plugin-field">
          <label className="setting">
            <input type="checkbox" {...common} required={false} checked={value === true} onChange={(e) => onChange(e.target.checked)} />
            <span>{f.label}</span>
          </label>
          {f.help && (
            <div id={helpId} className="small muted">
              {f.help}
            </div>
          )}
          {error && (
            <div id={errId} className="small field-error">
              {error}
            </div>
          )}
        </div>
      );
    case "choice":
      if ((f.choices ?? []).length <= 4) {
        return (
          <div className="plugin-field" role="radiogroup" aria-labelledby={id + "-label"} aria-describedby={described}>
            <div id={id + "-label"}>{f.label}</div>
            {(f.choices ?? []).map((c, i) => (
              <label key={c.value} className="setting">
                <input type="radio" id={i === 0 ? id : undefined} name={id} value={c.value} checked={value === c.value} onChange={() => onChange(c.value)} />
                <span>{c.label}</span>
              </label>
            ))}
            {f.help && (
              <div id={helpId} className="small muted">
                {f.help}
              </div>
            )}
            {error && (
              <div id={errId} className="small field-error">
                {error}
              </div>
            )}
          </div>
        );
      }
      control = (
        <select {...common} value={String(value ?? "")} onChange={(e) => onChange(e.target.value)}>
          {(f.choices ?? []).map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </select>
      );
      break;
    case "secret":
      control = (
        <input
          type="password"
          {...common}
          required={f.required && !f.set}
          value={String(value ?? "")}
          placeholder={f.set ? "Stored. Leave empty to keep it." : f.placeholder}
          autoComplete="off"
          spellCheck={false}
          autoCapitalize="off"
          onChange={(e) => onChange(e.target.value)}
        />
      );
      break;
    default:
      control = (
        <input
          type={f.kind === "url" ? "url" : f.kind === "email" ? "email" : "text"}
          inputMode={f.kind === "url" ? "url" : undefined}
          {...common}
          value={String(value ?? "")}
          placeholder={f.placeholder}
          onChange={(e) => onChange(e.target.value)}
        />
      );
  }
  return (
    <div className="plugin-field">
      <label htmlFor={id}>{f.label}</label>
      {control}
      {f.help && (
        <div id={helpId} className="small muted">
          {f.help}
        </div>
      )}
      {error && (
        <div id={errId} className="small field-error">
          {error}
        </div>
      )}
    </div>
  );
}
