package plugin

// What kalka lists about its plugins (spec 10.2, 10.6): every installed
// plugin with where it was found and what it declares, and, for doctor,
// each plugin's checked state.

import (
	"context"
	"errors"
	"sync"
)

// Listed is one installed plugin. Problem replaces Name, Version and
// Capabilities when describe failed.
type Listed struct {
	Scheme       string        `json:"scheme"`
	Name         string        `json:"name,omitempty"`
	Version      string        `json:"version,omitempty"`
	File         string        `json:"file"`
	Source       string        `json:"source"`
	Shadowed     []string      `json:"shadowed"`
	Capabilities *Capabilities `json:"capabilities,omitempty"`
	Problem      string        `json:"problem,omitempty"`
}

// Dirs are the folders searched before PATH.
type Dirs struct {
	Config  string `json:"config"`
	Bundled string `json:"bundled"`
}

// List is the answer of GET /api/plugins.
type List struct {
	Dirs    Dirs     `json:"dirs"`
	Plugins []Listed `json:"plugins"`
}

// List describes every installed plugin. It runs no settings op.
func (h *Host) List(ctx context.Context) List {
	out := List{Dirs: Dirs{Config: h.Dir, Bundled: h.ExeDir}, Plugins: []Listed{}}
	for _, scheme := range h.Installed() {
		f, err := h.Locate(scheme)
		if err != nil {
			continue
		}
		l := Listed{Scheme: scheme, File: f.File, Source: f.Source, Shadowed: f.Shadowed}
		if d, err := h.describeCached(ctx, f.File); err != nil {
			l.Problem = text(err.Error(), maxText)
		} else {
			caps := d.Capabilities
			l.Name, l.Version, l.Capabilities = d.Name, d.Version, &caps
		}
		out.Plugins = append(out.Plugins, l)
	}
	return out
}

// Survey is one plugin's state for doctor: Problem when it cannot be
// described or its settings op failed; Settings false when it declares
// none; otherwise Status, checked against the service.
type Survey struct {
	Scheme   string  `json:"scheme"`
	Problem  string  `json:"problem,omitempty"`
	Settings bool    `json:"settings"`
	Status   *Status `json:"status,omitempty"`
	Hint     *Hint   `json:"hint,omitempty"`
}

// SurveyAll asks every installed plugin for its checked state, in
// parallel, each within the settings op's timeout.
func (h *Host) SurveyAll(ctx context.Context) []Survey {
	l := h.List(ctx)
	out := make([]Survey, len(l.Plugins))
	var wg sync.WaitGroup
	for i, p := range l.Plugins {
		out[i] = Survey{Scheme: p.Scheme, Problem: p.Problem}
		if p.Problem != "" || !p.Capabilities.CanSettings() {
			continue
		}
		out[i].Settings = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := h.Settings(ctx, p.Scheme, true)
			if err != nil {
				out[i].Problem = err.Error()
				if errors.Is(err, ErrNoSettings) {
					out[i].Problem, out[i].Settings = "", false
				}
				return
			}
			out[i].Status, out[i].Hint = &a.Status, a.Hint
		}()
	}
	wg.Wait()
	return out
}
