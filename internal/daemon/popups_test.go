package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/syncproto"
)

type shown struct {
	title, msg string
	ctx        context.Context
	reply      chan popupResult
}

func TestPopups(t *testing.T) {
	w := newWorld(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bp"), 0o755); err != nil {
		t.Fatal(err)
	}
	popped := make(chan shown, 4)
	opened := make(chan string, 4)
	d, err := New(Options{
		Config:    &config.Config{Email: "bob@corp.com", Rendezvous: w.url, Folder: filepath.Join(root, "bp"), UIPort: 1},
		ConfigDir: filepath.Join(root, "cfg"), Loopback: true, APIAddr: "127.0.0.1:0",
		OpenUI: func(u string) { opened <- u },
		Popup: func(ctx context.Context, title, msg string) (bool, error) {
			s := shown{title, msg, ctx, make(chan popupResult, 1)}
			popped <- s
			select {
			case r := <-s.reply:
				return r.open, r.err
			case <-ctx.Done():
				return false, ctx.Err()
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = d.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	<-d.Ready()

	ask := func(kind, title string) string {
		before := map[string]bool{}
		for _, p := range d.Prompts.List() {
			before[p.ID] = true
		}
		go d.Prompts.Ask(ctx, "", syncproto.Prompt{Kind: kind, Peer: "alice@corp.com", Title: title, Message: "?"})
		for i := 0; i < 200; i++ {
			for _, p := range d.Prompts.List() {
				if !before[p.ID] {
					return p.ID
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("prompt did not open")
		return ""
	}
	next := func() shown {
		t.Helper()
		select {
		case s := <-popped:
			return s
		case <-time.After(3 * time.Second):
			t.Fatal("no popup")
		}
		return shown{}
	}
	none := func(ch any) {
		t.Helper()
		time.Sleep(150 * time.Millisecond)
		switch c := ch.(type) {
		case chan shown:
			if len(c) > 0 {
				t.Fatal("unexpected popup")
			}
		case chan string:
			if len(c) > 0 {
				t.Fatal("unexpected browser open")
			}
		}
	}

	// One popup at a time; it says what waits, in plain words.
	a := ask(syncproto.PromptNew, `Plan "X"`)
	pa := next()
	if !strings.Contains(pa.title, "alice@corp.com") || !strings.Contains(pa.msg, `“Plan "X"”`) || strings.Contains(pa.msg, "?") {
		t.Fatalf("popup text %q / %q", pa.title, pa.msg)
	}
	b := ask(syncproto.PromptHandoff, "Y")
	none(popped)

	// Answering in the UI closes the popup; the next prompt gets one.
	_ = d.Prompts.Answer(a, true)
	select {
	case <-pa.ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("popup not closed when its prompt was answered")
	}
	pb := next()
	if !strings.Contains(pb.msg, "hand “Y” over") {
		t.Fatalf("second popup %q", pb.msg)
	}
	// "Open kalka" opens the UI with the token link.
	pb.reply <- popupResult{open: true}
	select {
	case u := <-opened:
		if u != d.URL() {
			t.Fatalf("opened %q", u)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("UI not opened")
	}
	_ = d.Prompts.Answer(b, false)

	// "Later" opens nothing and does not ask again for that prompt.
	c := ask(syncproto.PromptVerify, "")
	next().reply <- popupResult{}
	none(opened)
	none(popped)
	_ = d.Prompts.Answer(c, false)

	// No popup while a UI page is connected.
	_, unsub := d.bus.Subscribe()
	e := ask(syncproto.PromptNew, "Z")
	none(popped)
	_ = d.Prompts.Answer(e, false)
	unsub()

	// A system without popups falls back to opening the browser.
	f := ask(syncproto.PromptNew, "W")
	next().reply <- popupResult{err: errors.New("no popup available")}
	select {
	case <-opened:
	case <-time.After(3 * time.Second):
		t.Fatal("no fallback")
	}
	_ = d.Prompts.Answer(f, false)
}
