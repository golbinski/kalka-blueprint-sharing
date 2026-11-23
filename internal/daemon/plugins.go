package daemon

// Plugin settings (spec 10.6, 13): list the installed plugins, show a
// plugin's state and form, apply and clear its settings. The daemon runs
// the plugin for each request and keeps none of what it is given or
// answers; no request body is logged, and no error repeats a value.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/plugin"
)

// PluginSettings is what the daemon needs of the plugin host beyond
// core.Plugins; *plugin.Host has it.
type PluginSettings interface {
	List(ctx context.Context) plugin.List
	Settings(ctx context.Context, scheme string, check bool) (*plugin.Answer, error)
	Apply(ctx context.Context, scheme string, values map[string]any, secrets map[string]plugin.Secret) (*plugin.Answer, error)
	Clear(ctx context.Context, scheme string) (*plugin.Answer, error)
	SurveyAll(ctx context.Context) []plugin.Survey
}

// pluginLocks serialises changes per scheme; reads take no lock.
var pluginLocks sync.Map

func pluginLock(scheme string) *sync.Mutex {
	m, _ := pluginLocks.LoadOrStore(scheme, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func (d *Daemon) pluginHost() PluginSettings {
	if p, ok := d.opts.Plugins.(PluginSettings); ok {
		return p
	}
	return nil
}

// pluginStatus is the HTTP status of a plugin settings error.
func pluginStatus(err error) int {
	switch core.KindOf(err) {
	case core.KindNoPlugin:
		return http.StatusNotFound
	case core.KindNoPluginSettings:
		return http.StatusConflict
	case core.KindPluginValue:
		return http.StatusBadRequest
	case core.KindPluginRejected:
		return http.StatusUnprocessableEntity
	case core.KindPluginFailed:
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}

func (d *Daemon) mountPlugins(mux *http.ServeMux) {
	noPlugin := func(w http.ResponseWriter) {
		fail(w, http.StatusNotFound, plugin.ErrNotFound)
	}
	answer := func(w http.ResponseWriter, a *plugin.Answer, err error) {
		if err != nil {
			fail(w, pluginStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, a)
	}
	mux.HandleFunc("GET /api/plugins", func(w http.ResponseWriter, r *http.Request) {
		h := d.pluginHost()
		if h == nil {
			writeJSON(w, http.StatusOK, plugin.List{Plugins: []plugin.Listed{}})
			return
		}
		writeJSON(w, http.StatusOK, h.List(r.Context()))
	})
	mux.HandleFunc("GET /api/plugins/{scheme}/settings", func(w http.ResponseWriter, r *http.Request) {
		h := d.pluginHost()
		if h == nil {
			noPlugin(w)
			return
		}
		a, err := h.Settings(r.Context(), r.PathValue("scheme"), r.URL.Query().Get("check") == "1")
		answer(w, a, err)
	})
	mux.HandleFunc("PUT /api/plugins/{scheme}/settings", func(w http.ResponseWriter, r *http.Request) {
		h := d.pluginHost()
		if h == nil {
			noPlugin(w)
			return
		}
		var q struct {
			Values  map[string]any    `json:"values"`
			Secrets map[string]string `json:"secrets"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		dec := json.NewDecoder(r.Body)
		dec.UseNumber()
		if err := dec.Decode(&q); err != nil {
			// The decoder's message may quote the body.
			fail(w, http.StatusBadRequest, errors.New("bad request body: expected {\"values\": {...}, \"secrets\": {...}}"))
			return
		}
		secrets := make(map[string]plugin.Secret, len(q.Secrets))
		for k, v := range q.Secrets {
			secrets[k] = plugin.Secret(v)
		}
		scheme := r.PathValue("scheme")
		mu := pluginLock(scheme)
		mu.Lock()
		defer mu.Unlock()
		a, err := h.Apply(r.Context(), scheme, q.Values, secrets)
		if err == nil {
			d.PluginChanged(scheme)
		}
		answer(w, a, err)
	})
	mux.HandleFunc("DELETE /api/plugins/{scheme}/settings", func(w http.ResponseWriter, r *http.Request) {
		h := d.pluginHost()
		if h == nil {
			noPlugin(w)
			return
		}
		scheme := r.PathValue("scheme")
		mu := pluginLock(scheme)
		mu.Lock()
		defer mu.Unlock()
		a, err := h.Clear(r.Context(), scheme)
		if err == nil {
			d.PluginChanged(scheme)
		}
		answer(w, a, err)
	})
	mux.HandleFunc("POST /api/plugins/{scheme}/refresh", func(w http.ResponseWriter, r *http.Request) {
		h := d.pluginHost()
		if h == nil {
			noPlugin(w)
			return
		}
		scheme := r.PathValue("scheme")
		if _, err := d.opts.Plugins.Find(scheme); err != nil {
			noPlugin(w)
			return
		}
		mu := pluginLock(scheme)
		mu.Lock()
		d.PluginChanged(scheme)
		mu.Unlock()
		a, err := h.Settings(r.Context(), scheme, false)
		answer(w, a, err)
	})
}
