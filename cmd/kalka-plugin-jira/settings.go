package main

// The settings ops (spec 10.6): kalka shows the form described here and
// passes on what the person enters; the plugin checks it against Jira and
// stores it as it always has (store.go): the site, flavor and email in
// config.json, the token in the system's credential store, or in
// config.json, readable by the user only, where there is none.

import (
	"context"
	"strings"
	"time"
)

// setupHint is how a person sets the plugin up.
const setupHint = "set it up in kalka's Settings, under Plugins, or run `kalka plugin set jira`"

// checkBudget is how long a settings check may ask Jira, inside kalka's
// 10 s; applyBudget how long an apply may, inside kalka's 30 s.
var (
	checkBudget = 8 * time.Second
	applyBudget = 20 * time.Second
)

type status struct {
	State   string `json:"state"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
	Checked bool   `json:"checked"`
}

// settingsAnswer is the state and the form of the plugin.
func settingsAnswer(st *store, check bool, tweak func(*client)) map[string]any {
	cfg, err := st.load()
	stored := cfg != nil && err == nil
	var s status
	switch {
	case err == errNotLoggedIn:
		s = status{State: "needs-setup", Message: "Not connected to Jira."}
	case err != nil:
		s = status{State: "error", Message: err.Error()}
	default:
		s = readyStatus(st, cfg)
		if _, terr := st.token(cfg); terr != nil {
			s = status{State: "error", Message: terr.Error()}
		} else if check {
			c, cerr := connect(st)
			if cerr == nil {
				if tweak != nil {
					tweak(c)
				}
				ctx, cancel := context.WithTimeout(context.Background(), checkBudget)
				cerr = c.myself(ctx)
				cancel()
			}
			if cerr != nil {
				s = status{State: "error", Message: cerr.Error(), Detail: s.Detail}
			}
			s.Checked = true
		}
	}
	if !stored {
		cfg = &siteConfig{Flavor: Cloud}
	}
	return map[string]any{"protocol": protocol, "status": s, "fields": fields(cfg, stored)}
}

func readyStatus(st *store, cfg *siteConfig) status {
	msg := cfg.Site
	if cfg.Flavor != DataCenter && cfg.Email != "" {
		msg += " as " + cfg.Email
	}
	detail := "The token is in the system's credential store."
	if cfg.TokenIn == "file" {
		detail = "The token is in " + st.path() + " (readable by you only)."
	}
	return status{State: "ready", Message: msg, Detail: detail}
}

func fields(cfg *siteConfig, stored bool) []map[string]any {
	flavor := cfg.Flavor
	if flavor == "" {
		flavor = Cloud
	}
	site := map[string]any{"id": "site", "kind": "url", "label": "Site", "required": true, "placeholder": "https://example.atlassian.net"}
	if cfg.Site != "" {
		site["value"] = cfg.Site
	}
	email := map[string]any{"id": "email", "kind": "email", "label": "Account email", "required": true,
		"when": map[string]any{"field": "flavor", "is": []string{Cloud}}}
	if cfg.Email != "" {
		email["value"] = cfg.Email
	}
	return []map[string]any{
		{"id": "flavor", "kind": "choice", "label": "Jira", "required": true, "value": flavor,
			"choices": []map[string]string{{"value": Cloud, "label": "Jira Cloud"}, {"value": DataCenter, "label": "Jira Data Center or Server"}}},
		site,
		email,
		{"id": "token", "kind": "secret", "label": "API token", "required": true, "set": stored,
			"help": "Jira Cloud: an API token from id.atlassian.com, Security, API tokens. Data Center: a personal access token from your profile."},
	}
}

// applySettings checks the merged settings against Jira and stores them;
// on any failure nothing stored changes.
func applySettings(st *store, values map[string]any, secrets map[string]string, tweak func(*client)) map[string]any {
	fail := func(msg string, fe map[string]string) map[string]any {
		out := map[string]any{"protocol": protocol, "ok": false}
		if msg != "" {
			out["error"] = msg
		}
		if len(fe) > 0 {
			out["field_errors"] = fe
		}
		return out
	}
	old, err := st.load()
	if err != nil && err != errNotLoggedIn {
		return fail(err.Error(), nil)
	}
	cfg := &siteConfig{Flavor: Cloud}
	if old != nil && err == nil {
		cfg = &siteConfig{Site: old.Site, Flavor: old.Flavor, Email: old.Email}
		if cfg.Flavor == "" {
			cfg.Flavor = Cloud
		}
	}
	str := func(k string) (string, bool) {
		v, ok := values[k]
		if !ok {
			return "", false
		}
		s, _ := v.(string)
		return strings.TrimSpace(s), true
	}
	fe := map[string]string{}
	if v, ok := str("flavor"); ok {
		if v != Cloud && v != DataCenter {
			fe["flavor"] = "choose Jira Cloud or Data Center"
		}
		cfg.Flavor = v
	}
	if v, ok := str("site"); ok {
		cfg.Site = v
	}
	if v, ok := str("email"); ok {
		cfg.Email = v
	}
	if cfg.Flavor == DataCenter {
		cfg.Email = ""
	}
	if cfg.Site == "" {
		fe["site"] = "enter your Jira site, such as https://example.atlassian.net"
	} else if clean, err := checkSite(cfg.Site); err != nil {
		fe["site"] = err.Error()
	} else {
		cfg.Site = clean
	}
	if cfg.Flavor == Cloud && !strings.Contains(cfg.Email, "@") {
		fe["email"] = "Jira Cloud needs your account's email address"
	}
	token, given := secrets["token"]
	token = strings.TrimSpace(token)
	switch {
	case given && !tokenRe.MatchString(token):
		fe["token"] = "that does not look like an API token"
	case !given && (old == nil || old.Site != cfg.Site || old.Flavor != cfg.Flavor):
		// A stored token goes only to the site it was stored for.
		fe["token"] = "enter the token for this site"
	case !given:
		t, err := st.token(old)
		if err != nil {
			fe["token"] = err.Error()
		}
		token = t
	}
	if len(fe) > 0 {
		return fail("", fe)
	}
	c := newClient(cfg, token)
	if tweak != nil {
		tweak(c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), applyBudget)
	defer cancel()
	if err := c.myself(ctx); err != nil {
		return fail("Jira did not accept the credentials: "+err.Error(), map[string]string{"token": "check that the token is current and belongs to this account"})
	}
	if _, err := st.login(cfg, token); err != nil {
		return fail(err.Error(), nil)
	}
	return answerOK(st, true)
}

// clearSettings removes the stored settings; with none it is done.
func clearSettings(st *store) map[string]any {
	if err := st.logout(); err != nil && err != errNotLoggedIn {
		return map[string]any{"protocol": protocol, "ok": false, "error": err.Error()}
	}
	return answerOK(st, false)
}

func answerOK(st *store, checked bool) map[string]any {
	st.cfg = nil
	out := settingsAnswer(st, false, nil)
	if s, ok := out["status"].(status); ok && checked && s.State == "ready" {
		s.Checked = true
		out["status"] = s
	}
	out["ok"] = true
	return out
}
