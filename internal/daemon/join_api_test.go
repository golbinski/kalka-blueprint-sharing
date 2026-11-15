package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/teamlog"
)

// TestJoinPreviewAPI: POST /api/joins/preview answers what the join
// screen shows and never joins (spec 13, 19.7).
func TestJoinPreviewAPI(t *testing.T) {
	w := newWorld(t)
	a := w.node("ewa@parcels.example")
	c := NewClient(a.d.APIAddr(), a.d.Token())
	ctx := context.Background()
	if _, err := c.CreateTeam(ctx, "parcel-ops"); err != nil {
		t.Fatal(err)
	}
	inv, err := c.Invite(ctx, "parcel-ops", "olga@parcels.example", "")
	if err != nil || inv.Link != teamlog.LinkPrefix+inv.Token {
		t.Fatalf("invite %+v %v", inv, err)
	}
	p, err := c.JoinPreview(ctx, "<"+inv.Link+">")
	if err != nil || p.Problem != core.ProblemOwnInvite || p.Inviter != "ewa@parcels.example" || p.Name != "parcel-ops" {
		t.Fatalf("preview %+v %v", p, err)
	}
	var re *RemoteError
	if _, err := c.JoinPreview(ctx, inv.Link+"?x"); !errors.As(err, &re) || re.Kind != "invite-token" {
		t.Fatalf("bad link: %v", err)
	}
	if js, _ := c.Joins(ctx); len(js) != 0 {
		t.Fatal("a preview joined")
	}
	if _, err := c.Join(ctx, inv.Link); !errors.As(err, &re) || re.Kind != "own-invite" {
		t.Fatalf("join own invite: %v", err)
	}
}

// TestSetupPreview: before setup, the preview reads the token alone, so
// the form opened from a link fills in the email and the rendezvous.
func TestSetupPreview(t *testing.T) {
	t.Setenv(config.DirEnv, filepath.Join(t.TempDir(), "cfg"))
	done := make(chan *config.Config, 1)
	var port string
	hs := httptest.NewServer(SetupHandler("tok", func() string { return port }, done))
	defer hs.Close()
	u, _ := url.Parse(hs.URL)
	port = u.Port()

	pub, _, _ := ed25519.GenerateKey(nil)
	iv := teamlog.Invite{TeamName: "parcel-ops", Team: strings.Repeat("ab", 32), Owner: teamlog.EncodeKey(pub),
		Inviter: "ewa@parcels.example", Email: "olga@parcels.example", Secret: bytes.Repeat([]byte{9}, 32),
		Expires: time.Now().Add(time.Hour).UTC().Truncate(time.Second), Rendezvous: "wss://rdv.parcels.example/"}
	post := func(token string) (int, core.JoinPreview) {
		body, _ := json.Marshal(TeamRequest{Token: token})
		req, _ := http.NewRequest(http.MethodPost, hs.URL+"/api/joins/preview", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer tok")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var p core.JoinPreview
		_ = json.NewDecoder(resp.Body).Decode(&p)
		return resp.StatusCode, p
	}
	code, p := post(iv.Link())
	if code != 200 || p.Email != "olga@parcels.example" || p.Rendezvous != "wss://rdv.parcels.example/" || p.Inviter != "ewa@parcels.example" {
		t.Fatalf("%d %+v", code, p)
	}
	if code, _ := post("kalka://join/nope"); code != 400 {
		t.Fatalf("junk: %d", code)
	}
}
