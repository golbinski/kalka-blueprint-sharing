package daemon

import (
	"context"
	"errors"
	"fmt"
	"github.com/golbinski/kalka/internal/core"
	"time"

	"github.com/golbinski/kalka/internal/syncproto"
)

// popups brings the UI to a person when a teammate's sync waits for an
// answer and no kalka page is open (implementation plan A8, Q3). The
// daemon runs in the background with no terminal, so it shows a native
// popup, "Open kalka" or "Later"; the answer itself is given in the UI.
// One popup shows at a time; it closes when its prompt is answered or
// times out, and the next unanswered prompt, if any, gets one.
//
// All methods run on the forward goroutine.
type popups struct {
	d        *Daemon
	results  chan popupResult
	active   string // prompt id with a popup showing
	cancel   context.CancelFunc
	later    map[string]bool // prompts the user put off
	lastOpen time.Time
}

type popupResult struct {
	id   string
	open bool
	err  error
}

func newPopups(d *Daemon) *popups {
	return &popups{d: d, results: make(chan popupResult, 1), later: map[string]bool{}}
}

func (p *popups) opened(ctx context.Context, pr *core.Pending) {
	if pr.Scope != "" || p.active != "" || p.d.Bus().Clients() > 0 || p.d.Bus().Shells() > 0 {
		return
	}
	if p.d.opts.Popup == nil {
		p.fallback()
		return
	}
	pctx, cancel := context.WithCancel(ctx)
	p.active, p.cancel = pr.ID, cancel
	title, msg := popupText(pr.Prompt)
	go func() {
		ok, err := p.d.opts.Popup(pctx, title, msg)
		p.results <- popupResult{pr.ID, ok, err}
	}()
}

func (p *popups) closed(pr *core.Pending) {
	delete(p.later, pr.ID)
	if pr.ID == p.active {
		p.cancel()
	}
}

func (p *popups) finished(ctx context.Context, r popupResult) {
	p.cancel()
	p.active, p.cancel = "", nil
	switch {
	case r.open:
		p.openUI()
		return
	case r.err != nil && ctx.Err() == nil && !errors.Is(r.err, context.Canceled):
		p.d.Logf("popup failed (%v); opening the browser", r.err)
		p.fallback()
		return
	}
	if ctx.Err() != nil {
		return
	}
	// "Later", or the prompt closed under the popup: move on to the next
	// prompt still waiting that the user has not put off.
	if r.err == nil {
		p.later[r.id] = true
	}
	for _, pr := range p.d.Prompts.List() {
		if pr.Scope == "" && !p.later[pr.ID] {
			p.opened(ctx, pr)
			return
		}
	}
}

func (p *popups) openUI() {
	if p.d.opts.OpenUI != nil {
		p.d.opts.OpenUI(p.d.URL())
	}
}

// fallback opens the browser without asking, at most once a minute, so a
// burst of prompts does not open a burst of tabs.
func (p *popups) fallback() {
	if time.Since(p.lastOpen) < time.Minute {
		return
	}
	p.lastOpen = time.Now()
	p.openUI()
}

func (p *popups) stop() {
	if p.cancel != nil {
		p.cancel()
	}
}

// popupText says what waits, without asking the question the UI asks:
// the popup's buttons only open kalka or put it off.
func popupText(pr syncproto.Prompt) (title, message string) {
	switch pr.Kind {
	case syncproto.PromptVerify:
		title = "kalka: " + pr.Peer + " is syncing with you"
		message = fmt.Sprintf("%s is syncing with you for the first time. Compare the verification code with them in kalka.", pr.Peer)
	case syncproto.PromptNew:
		title = "kalka: new blueprint from " + pr.Peer
		message = fmt.Sprintf("%s sends you a new blueprint: “%s”.", pr.Peer, pr.Title)
	case syncproto.PromptHandoff:
		title = "kalka: handoff from " + pr.Peer
		message = fmt.Sprintf("%s wants to hand “%s” over to you.", pr.Peer, pr.Title)
	default:
		title = "kalka: " + pr.Peer + " is waiting"
		message = pr.Message
	}
	return title, message + fmt.Sprintf("\n\n%s waits for your answer for up to %s.", pr.Peer, syncproto.PromptTimeout.Round(time.Minute))
}
