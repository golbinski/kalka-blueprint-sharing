package plugin

// Settings (spec 10.6): a plugin that declares the settings capability
// describes a form and its state; kalka shows the form, checks what the
// person enters against it, and hands it to the plugin, which validates
// and stores it where it always has (the system's credential store, its
// own files, another tool's login). kalka keeps no value and no secret,
// and no answer it gives back carries a stored secret.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// SettingsTimeout is how long settings_apply may take: it validates
// against a remote service. settings and settings_clear take Timeout.
var SettingsTimeout = 30 * time.Second

// SettingsCaps is the settings capability: the plugin answers the
// settings op, and settings_apply and settings_clear when declared.
type SettingsCaps struct {
	Apply bool `json:"apply,omitempty"`
	Clear bool `json:"clear,omitempty"`
}

// CanSettings reports the settings op.
func (c Capabilities) CanSettings() bool { return c.Settings != nil }

// CanApply reports settings_apply.
func (c Capabilities) CanApply() bool { return c.Settings != nil && c.Settings.Apply }

// CanClear reports settings_clear.
func (c Capabilities) CanClear() bool { return c.Settings != nil && c.Settings.Clear }

// Plugin states (spec 10.6).
const (
	StateReady      = "ready"
	StateNeedsSetup = "needs-setup"
	StateError      = "error"
)

// Field kinds.
const (
	KindText   = "text"
	KindURL    = "url"
	KindEmail  = "email"
	KindSecret = "secret"
	KindChoice = "choice"
	KindBool   = "bool"
)

// Limits of a form (spec 10.6).
const (
	MaxFields      = 16
	MaxChoices     = 20
	MaxValue       = 2048
	MaxEmail       = 254
	MaxSecret      = 4096
	maxStatusMsg   = 200
	maxStatusExtra = 300
	maxLabel       = 80
	maxHelp        = 300
	maxPlaceholder = 100
	maxHintCommand = 200
	maxApplyError  = 500
	maxFieldError  = 300
)

var (
	fieldIDRe     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	choiceValueRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
)

// Status is a plugin's state: ready, needs-setup or error, in its words.
type Status struct {
	State   string `json:"state"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
	// Checked: the state was confirmed against the service in this call.
	Checked bool `json:"checked"`
}

// Choice is one value of a choice field.
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// When shows a field only while an earlier choice or bool field holds one
// of Is (strings for a choice, booleans for a bool).
type When struct {
	Field string `json:"field"`
	Is    []any  `json:"is"`
}

// Field is one field of a plugin's form. A secret field never carries a
// Value, only Set.
type Field struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Label       string   `json:"label"`
	Help        string   `json:"help,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Value       any      `json:"value,omitempty"`
	Set         *bool    `json:"set,omitempty"`
	Choices     []Choice `json:"choices,omitempty"`
	When        *When    `json:"when,omitempty"`
}

// Hint points at a step kalka does not take itself. Command is shown,
// never run.
type Hint struct {
	Text    string `json:"text"`
	Command string `json:"command,omitempty"`
	URL     string `json:"url,omitempty"`
}

// SettingsView is a plugin's state and form, cleaned. Incomplete: the
// plugin sent fields this kalka cannot show, so nothing may be applied.
type SettingsView struct {
	Status     Status  `json:"status"`
	Fields     []Field `json:"fields"`
	Hint       *Hint   `json:"hint"`
	Incomplete bool    `json:"incomplete"`
}

// Answer is a plugin's settings as kalka shows them: the cleaned view,
// and what can be done with it (apply is false for an incomplete form).
type Answer struct {
	Scheme string `json:"scheme"`
	SettingsView
	Can Can `json:"can"`
}

// Can says whether the settings can be applied and cleared.
type Can struct {
	Apply bool `json:"apply"`
	Clear bool `json:"clear"`
}

// FailedError: the plugin crashed, timed out, or answered with something
// else than the expected answer.
type FailedError struct{ Msg string }

func (e *FailedError) Error() string { return e.Msg }

// RejectedError: the plugin refused the settings and stored nothing.
// Fields are its messages for fields of the form.
type RejectedError struct {
	Msg    string
	Fields map[string]string
}

func (e *RejectedError) Error() string { return e.Msg }

// Secret is a value of a secret field. It prints as [secret] in every
// form fmt and encoding/json give it; only the request to the plugin
// carries its bytes.
type Secret string

func (Secret) String() string               { return "[secret]" }
func (Secret) GoString() string             { return "[secret]" }
func (Secret) Format(f fmt.State, _ rune)   { io.WriteString(f, "[secret]") }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[secret]"`), nil }
func (Secret) MarshalText() ([]byte, error) { return []byte("[secret]"), nil }
func (s Secret) reveal() string             { return string(s) }

// Errors of the settings ops. ErrNoSettings: the plugin does not declare
// what was asked; ErrIncomplete: its form has fields this kalka cannot
// show, so it cannot be applied.
var (
	ErrNoSettings = errors.New("the plugin has no settings that can be changed here")
	ErrIncomplete = errors.New("this plugin has settings this kalka cannot show")
)

// ValueError says which values do not fit the plugin's form. The plugin
// was not asked to apply them.
type ValueError struct {
	Fields map[string]string
}

func (e *ValueError) Error() string {
	if len(e.Fields) == 1 {
		for id, m := range e.Fields {
			return id + ": " + m
		}
	}
	return "some values do not fit the plugin's settings"
}

// wire shapes

type wireField struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Label       string   `json:"label"`
	Help        string   `json:"help"`
	Placeholder string   `json:"placeholder"`
	Required    bool     `json:"required"`
	Value       rawValue `json:"value"`
	Set         bool     `json:"set"`
	Choices     []Choice `json:"choices"`
	When        *struct {
		Field string `json:"field"`
		Is    []any  `json:"is"`
	} `json:"when"`
}

// rawValue keeps a field's value as decoded, of whatever JSON type.
type rawValue struct {
	v   any
	set bool
}

func (r *rawValue) UnmarshalJSON(b []byte) error {
	r.set = true
	return json.Unmarshal(b, &r.v)
}

type settingsResponse struct {
	Protocol    int               `json:"protocol"`
	OK          *bool             `json:"ok"`
	Error       string            `json:"error"`
	FieldErrors map[string]string `json:"field_errors"`
	Status      *Status           `json:"status"`
	Fields      []wireField       `json:"fields"`
	Hint        *Hint             `json:"hint"`
}

// cleanView turns a plugin's answer into what the core shows: states
// normalized, text capped, unknown or broken fields dropped, secret values
// stripped (spec 10.6).
func (h *Host) cleanView(plugin string, r settingsResponse) *SettingsView {
	v := &SettingsView{Fields: []Field{}}
	if r.Status != nil {
		v.Status = *r.Status
	} else {
		v.Status = Status{State: StateError, Message: "the plugin gave no status"}
	}
	switch v.Status.State {
	case StateReady, StateNeedsSetup, StateError:
	default:
		v.Status.State = StateError
	}
	v.Status.Message = oneLine(v.Status.Message, maxStatusMsg)
	v.Status.Detail = oneLine(v.Status.Detail, maxStatusExtra)

	seen := map[string]bool{}
	kinds := map[string]Field{}
	for _, wf := range r.Fields {
		f, ok, broken := h.cleanField(plugin, wf, kinds)
		if seen[wf.ID] {
			ok, broken = false, true
		}
		seen[wf.ID] = true
		if ok && len(v.Fields) >= MaxFields {
			ok = false
		}
		if !ok {
			if broken || wf.Required {
				v.Incomplete = true
			}
			continue
		}
		v.Fields = append(v.Fields, f)
		kinds[f.ID] = f
	}
	if r.Hint != nil && strings.TrimSpace(r.Hint.Text) != "" {
		hint := Hint{Text: oneLine(r.Hint.Text, maxHelp), URL: httpURL(r.Hint.URL)}
		if c := r.Hint.Command; c != "" && printableLine(c) && len(c) <= maxHintCommand {
			hint.Command = c
		}
		v.Hint = &hint
	}
	return v
}

// cleanField checks one field. ok false drops it; broken says it was
// dropped for a broken rule rather than for an unknown kind.
func (h *Host) cleanField(plugin string, wf wireField, earlier map[string]Field) (f Field, ok, broken bool) {
	switch wf.Kind {
	case KindText, KindURL, KindEmail, KindSecret, KindChoice, KindBool:
	default:
		return f, false, false
	}
	if !fieldIDRe.MatchString(wf.ID) || strings.TrimSpace(wf.Label) == "" {
		return f, false, true
	}
	f = Field{ID: wf.ID, Kind: wf.Kind, Label: oneLine(wf.Label, maxLabel), Help: oneLine(wf.Help, maxHelp), Required: wf.Required}
	if wf.Kind != KindChoice && wf.Kind != KindBool {
		f.Placeholder = oneLine(wf.Placeholder, maxPlaceholder)
	}
	if wf.Kind == KindChoice {
		vals := map[string]bool{}
		for _, c := range wf.Choices {
			if len(f.Choices) >= MaxChoices || !choiceValueRe.MatchString(c.Value) || vals[c.Value] {
				continue
			}
			vals[c.Value] = true
			label := oneLine(c.Label, maxLabel)
			if strings.TrimSpace(label) == "" {
				label = c.Value
			}
			f.Choices = append(f.Choices, Choice{Value: c.Value, Label: label})
		}
		if len(f.Choices) == 0 {
			return f, false, true
		}
	}
	if wf.Kind == KindSecret {
		if wf.Value.set && wf.Value.v != nil {
			h.logf("%s: sent the value of secret field %s; dropped", plugin, wf.ID)
		}
		set := wf.Set
		f.Set = &set
	} else if wf.Value.set && wf.Value.v != nil {
		switch wf.Kind {
		case KindBool:
			b, isBool := wf.Value.v.(bool)
			if !isBool {
				return f, false, true
			}
			f.Value = b
		case KindChoice:
			s, isStr := wf.Value.v.(string)
			if !isStr {
				return f, false, true
			}
			for _, c := range f.Choices {
				if c.Value == s {
					f.Value = s
				}
			}
		default:
			s, isStr := wf.Value.v.(string)
			if !isStr || checkPlain(wf.Kind, s) != "" {
				return f, false, true
			}
			f.Value = s
		}
	}
	if wf.When != nil {
		ctl, known := earlier[wf.When.Field]
		if !known || ctl.When != nil || (ctl.Kind != KindChoice && ctl.Kind != KindBool) || len(wf.When.Is) == 0 || len(wf.When.Is) > MaxChoices {
			return f, false, true
		}
		for _, x := range wf.When.Is {
			switch ctl.Kind {
			case KindBool:
				if _, isBool := x.(bool); !isBool {
					return f, false, true
				}
			case KindChoice:
				s, isStr := x.(string)
				if !isStr || !choiceValueRe.MatchString(s) {
					return f, false, true
				}
			}
		}
		f.When = &When{Field: wf.When.Field, Is: wf.When.Is}
	}
	return f, true, false
}

// Visible reports whether a field is shown when the form's fields hold
// values (the stored ones overlaid with those being entered).
func Visible(f Field, values map[string]any) bool {
	if f.When == nil {
		return true
	}
	cur := values[f.When.Field]
	for _, x := range f.When.Is {
		if x == cur {
			return true
		}
	}
	return false
}

// Current is the form's values as stored: every field but secrets.
func (v *SettingsView) Current() map[string]any {
	m := map[string]any{}
	for _, f := range v.Fields {
		if f.Kind != KindSecret && f.Value != nil {
			m[f.ID] = f.Value
		}
	}
	return m
}

// Field returns a field by id.
func (v *SettingsView) Field(id string) (Field, bool) {
	for _, f := range v.Fields {
		if f.ID == id {
			return f, true
		}
	}
	return Field{}, false
}

// CheckValues checks values and secrets against a form before they go to
// the plugin (spec 10.6): known ids, the right map for the kind, visible
// fields, and each kind's value rules. Secrets are trimmed of white space
// at both ends. It returns the secrets as they will be sent, or a
// *ValueError.
func CheckValues(v *SettingsView, values map[string]any, secrets map[string]Secret) (map[string]Secret, error) {
	errs := map[string]string{}
	eff := v.Current()
	for id, x := range values {
		eff[id] = x
	}
	for id, x := range values {
		f, ok := v.Field(id)
		switch {
		case !ok:
			errs[id] = "the plugin has no such field"
		case f.Kind == KindSecret:
			errs[id] = "a secret is entered as a secret, not as a value"
		case !Visible(f, eff):
			errs[id] = "this field is not used with the current choices"
		default:
			if m := checkValue(f, x); m != "" {
				errs[id] = m
			}
		}
	}
	out := map[string]Secret{}
	for id, s := range secrets {
		f, ok := v.Field(id)
		switch {
		case !ok:
			errs[id] = "the plugin has no such field"
			continue
		case f.Kind != KindSecret:
			errs[id] = "this field is not a secret"
			continue
		case !Visible(f, eff):
			errs[id] = "this field is not used with the current choices"
			continue
		}
		t := strings.Trim(s.reveal(), " \t\r\n")
		switch {
		case t == "":
			errs[id] = "enter a value"
		case len(t) > MaxSecret:
			errs[id] = fmt.Sprintf("longer than %d bytes", MaxSecret)
		case !utf8.ValidString(t) || hasControl(t):
			errs[id] = "holds characters that are not allowed"
		default:
			out[id] = Secret(t)
		}
	}
	if len(errs) > 0 {
		return nil, &ValueError{Fields: errs}
	}
	return out, nil
}

func checkValue(f Field, x any) string {
	switch f.Kind {
	case KindBool:
		if _, ok := x.(bool); !ok {
			return "must be true or false"
		}
	case KindChoice:
		s, ok := x.(string)
		if !ok {
			return "must be one of the choices"
		}
		for _, c := range f.Choices {
			if c.Value == s {
				return ""
			}
		}
		return "must be one of the choices"
	default:
		s, ok := x.(string)
		if !ok {
			return "must be text"
		}
		return checkPlain(f.Kind, s)
	}
	return ""
}

// checkPlain checks a text, url or email value; empty clears the field.
func checkPlain(kind, s string) string {
	if len(s) > MaxValue {
		return fmt.Sprintf("longer than %d bytes", MaxValue)
	}
	if !utf8.ValidString(s) || hasControl(s) {
		return "holds characters that are not allowed"
	}
	if s == "" {
		return ""
	}
	switch kind {
	case KindURL:
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return "must be an http or https address"
		}
	case KindEmail:
		at := strings.Index(s, "@")
		if len(s) > MaxEmail || at <= 0 || at == len(s)-1 || strings.Count(s, "@") != 1 {
			return "must be an email address"
		}
		if _, err := mail.ParseAddress(s); err != nil {
			return "must be an email address"
		}
	}
	return ""
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func printableLine(s string) bool { return utf8.ValidString(s) && !hasControl(s) }

func oneLine(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	return text(strings.TrimSpace(s), max)
}

// Settings asks the scheme's plugin for its state and form. check asks it
// to try its credentials against the service.
func (h *Host) Settings(ctx context.Context, scheme string, check bool) (*Answer, error) {
	path, d, err := h.settingsPlugin(ctx, scheme)
	if err != nil {
		return nil, err
	}
	if !d.Capabilities.CanSettings() {
		return nil, ErrNoSettings
	}
	v, err := h.view(ctx, path, check)
	if err != nil {
		return nil, err
	}
	return answer(scheme, d, v), nil
}

func answer(scheme string, d *Description, v *SettingsView) *Answer {
	return &Answer{Scheme: scheme, SettingsView: *v, Can: Can{Apply: d.Capabilities.CanApply() && !v.Incomplete, Clear: d.Capabilities.CanClear()}}
}

func (h *Host) view(ctx context.Context, path string, check bool) (*SettingsView, error) {
	var r settingsResponse
	if err := h.callOpts(ctx, path, Timeout, request{Protocol: Protocol, Op: "settings", Check: check}, &r, false); err != nil {
		return nil, h.failed(path, err)
	}
	if r.Protocol != Protocol {
		return nil, &FailedError{fmt.Sprintf("%s answered with protocol %d", filepath.Base(path), r.Protocol)}
	}
	return h.cleanView(filepath.Base(path), r), nil
}

func (h *Host) failed(path string, err error) error {
	h.logf("%s: %v", filepath.Base(path), err)
	return &FailedError{fmt.Sprintf("%s failed: %v", filepath.Base(path), err)}
}

// Apply checks values and secrets against the plugin's form and, when
// they fit, asks the plugin to apply them. A *ValueError means the plugin
// was not asked; a *RejectedError that it refused and stored nothing.
func (h *Host) Apply(ctx context.Context, scheme string, values map[string]any, secrets map[string]Secret) (*Answer, error) {
	path, d, err := h.settingsPlugin(ctx, scheme)
	if err != nil {
		return nil, err
	}
	if !d.Capabilities.CanApply() {
		return nil, ErrNoSettings
	}
	form, err := h.view(ctx, path, false)
	if err != nil {
		return nil, err
	}
	if form.Incomplete {
		return nil, ErrIncomplete
	}
	sec, err := CheckValues(form, values, secrets)
	if err != nil {
		return nil, err
	}
	if values == nil {
		values = map[string]any{}
	}
	plain := make(map[string]string, len(sec))
	for id, s := range sec {
		plain[id] = s.reveal()
	}
	var r settingsResponse
	if err := h.callOpts(ctx, path, SettingsTimeout, request{Protocol: Protocol, Op: "settings_apply", Values: values, Secrets: plain}, &r, true); err != nil {
		return nil, h.failed(path, err)
	}
	v, err := h.applied(filepath.Base(path), r, form, sec)
	if err != nil {
		return nil, err
	}
	return answer(scheme, d, v), nil
}

// Clear asks the scheme's plugin to remove its stored settings.
func (h *Host) Clear(ctx context.Context, scheme string) (*Answer, error) {
	path, d, err := h.settingsPlugin(ctx, scheme)
	if err != nil {
		return nil, err
	}
	if !d.Capabilities.CanClear() {
		return nil, ErrNoSettings
	}
	var r settingsResponse
	if err := h.callOpts(ctx, path, Timeout, request{Protocol: Protocol, Op: "settings_clear"}, &r, false); err != nil {
		return nil, h.failed(path, err)
	}
	v, err := h.applied(filepath.Base(path), r, nil, nil)
	if err != nil {
		return nil, err
	}
	return answer(scheme, d, v), nil
}

func (h *Host) settingsPlugin(ctx context.Context, scheme string) (string, *Description, error) {
	path, err := h.Find(scheme)
	if err != nil {
		return "", nil, err
	}
	d, err := h.describeCached(ctx, path)
	if err != nil {
		return "", nil, &FailedError{fmt.Sprintf("%s: %v", filepath.Base(path), err)}
	}
	return path, d, nil
}

// applied cleans an apply or clear answer and scrubs from it every secret
// that was sent, so a plugin echoing one cannot pass it on.
func (h *Host) applied(plugin string, r settingsResponse, form *SettingsView, sent map[string]Secret) (*SettingsView, error) {
	if r.Protocol != Protocol {
		return nil, &FailedError{fmt.Sprintf("%s answered with protocol %d", plugin, r.Protocol)}
	}
	if r.OK == nil {
		return nil, &FailedError{fmt.Sprintf("%s gave no ok in its answer", plugin)}
	}
	secrets := make([]string, 0, len(sent))
	for _, s := range sent {
		secrets = append(secrets, s.reveal())
	}
	scrub := func(s string) string {
		for _, x := range secrets {
			if x != "" {
				s = strings.ReplaceAll(s, x, "[secret]")
			}
		}
		return s
	}
	if *r.OK {
		v := h.cleanView(plugin, r)
		v.Status.Message, v.Status.Detail = scrub(v.Status.Message), scrub(v.Status.Detail)
		for i := range v.Fields {
			f := &v.Fields[i]
			f.Label, f.Help, f.Placeholder = scrub(f.Label), scrub(f.Help), scrub(f.Placeholder)
			if s, ok := f.Value.(string); ok {
				f.Value = scrub(s)
			}
		}
		if v.Hint != nil {
			v.Hint.Text, v.Hint.Command = scrub(v.Hint.Text), scrub(v.Hint.Command)
		}
		return v, nil
	}
	rej := &RejectedError{Msg: oneLine(scrub(r.Error), maxApplyError)}
	for id, m := range r.FieldErrors {
		if form != nil {
			if _, ok := form.Field(id); !ok {
				continue
			}
		} else if !fieldIDRe.MatchString(id) {
			continue
		}
		if rej.Fields == nil {
			rej.Fields = map[string]string{}
		}
		rej.Fields[id] = oneLine(scrub(m), maxFieldError)
	}
	if rej.Msg == "" {
		rej.Msg = "The plugin did not accept the settings."
	}
	return nil, rej
}

// Forget drops what is cached about the scheme's plugin, so the next
// question asks it again (after its settings changed).
func (h *Host) Forget(scheme string) {
	path, err := h.Find(scheme)
	if err != nil {
		return
	}
	h.descMu.Lock()
	delete(h.descs, path)
	h.descMu.Unlock()
}
