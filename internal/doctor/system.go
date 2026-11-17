package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/golbinski/kalka/internal/autostart"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/identity"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/peerjs"
	"github.com/golbinski/kalka/internal/urlscheme"
	"github.com/golbinski/kalka/internal/webview2"
)

// System is the real probes, except the daemon's, which its caller
// knows. It only reads: nothing is registered, written or started, but
// for a temporary file in the folder, removed at once.
func System(configDir string) Probes {
	return Probes{
		Config: func() (Config, error) {
			c, err := config.Load()
			if errors.Is(err, config.ErrMissing) {
				return Config{}, ErrNoConfig
			}
			if err != nil {
				return Config{}, err
			}
			return Config{Email: c.Email, Rendezvous: c.Rendezvous, Folder: c.Folder}, nil
		},
		Key: func() error {
			_, err := identity.LoadKey(configDir)
			return err
		},
		Rendezvous: AskRendezvous,
		Folder:     Writable,
		WebView2:   webview2.Has,
		Autostart: func() (bool, bool, error) {
			st, err := autostart.Get()
			return st.Enabled, st.Stale, err
		},
		URLScheme: urlSchemeHere,
	}
}

// AskRendezvous asks the server for its ICE configuration (/kalka/ice, a
// kalka rendezvous) or, for a stock PeerJS server, any answer at all. The
// PeerJS cloud offers its own TURN servers.
func AskRendezvous(ctx context.Context, server string) (Rendezvous, error) {
	srv, err := peerjs.ParseRendezvous(server)
	if err != nil {
		return Rendezvous{}, err
	}
	url := srv.HTTPOrigin() + "/kalka/ice"
	cloud := server == "peerjs-cloud"
	if cloud {
		url = srv.HTTPOrigin() + "/"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Rendezvous{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Rendezvous{}, err
	}
	defer resp.Body.Close()
	rv := Rendezvous{TURN: cloud}
	if d, err := http.ParseTime(resp.Header.Get("Date")); err == nil {
		rv.Date = d
	}
	if !cloud && resp.StatusCode == http.StatusOK {
		var body struct {
			ICEServers []struct {
				URLs any `json:"urls"`
			} `json:"iceServers"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body) == nil {
			for _, s := range body.ICEServers {
				rv.TURN = rv.TURN || strings.Contains(fmt.Sprint(s.URLs), "turn:") || strings.Contains(fmt.Sprint(s.URLs), "turns:")
			}
		}
	}
	return rv, nil
}

// Writable checks that dir is a folder kalka can write to.
func Writable(dir string) error {
	st, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("the folder does not exist")
	}
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return errors.New("not a folder")
	}
	f, err := os.CreateTemp(dir, "."+name.Product+"-doctor-*")
	if err != nil {
		return errors.New("not writable")
	}
	p := f.Name()
	f.Close()
	return os.Remove(p)
}

// urlSchemeHere reads the kalka:// registration and compares it with the
// desktop shell next to this binary.
func urlSchemeHere() (bool, string, error) {
	exe, err := autostart.Executable()
	if err != nil {
		return false, "", err
	}
	shell := exe
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.Base(exe), name.DesktopBinary+".exe") {
		shell = filepath.Join(filepath.Dir(exe), name.DesktopBinary+".exe")
	}
	st, err := urlscheme.Status(urlscheme.System(shell))
	if err != nil {
		return false, "", err
	}
	if !st.Registered {
		return false, st.Detail, nil
	}
	if runtime.GOOS == "windows" && !strings.EqualFold(st.Command, urlscheme.Command(shell)) {
		return false, "they open " + st.Command, nil
	}
	return true, "", nil
}
