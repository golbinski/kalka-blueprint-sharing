package cli

import (
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/identity"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/store"
)

// signerFromConfig loads the configured email and identity key.
func signerFromConfig() (string, bp.Signer, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", nil, err
	}
	if !bp.ValidEmail(cfg.Email) {
		return "", nil, fmt.Errorf("configured email %q is not an email address", cfg.Email)
	}
	dir, err := config.Dir()
	if err != nil {
		return "", nil, err
	}
	key, err := identity.LoadOrCreateKey(dir)
	if err != nil {
		return "", nil, err
	}
	return cfg.Email, func(m []byte) []byte { return ed25519.Sign(key, m) }, nil
}

// `kalka seed <parent> [--from <entry id>] --title <t> [-m <text>] [--why <text>]`
func cmdSeed(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", "", "id of the entry the seed starts from (usually a deviation)")
	title := fs.String("title", "", "the seed's title (required)")
	msg := fs.String("m", "", "the problem; defaults to the quoted --from entry")
	why := fs.String("why", "", "why it was split off")
	asJSON := fs.Bool("json", false, "machine-readable output")
	files, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(files) != 1 || *title == "" {
		fmt.Fprintln(stderr, "seed: name the parent file and give --title")
		return exitUsage
	}
	email, sign, err := signerFromConfig()
	if err != nil {
		fmt.Fprintln(stderr, "seed:", err)
		return exitProblems
	}
	parentPath := files[0]
	raw, err := os.ReadFile(parentPath)
	if err != nil {
		fmt.Fprintln(stderr, "seed:", err)
		return exitProblems
	}
	parent, err := bp.Parse(raw)
	if err != nil || parent.HasErrors() {
		fmt.Fprintf(stderr, "seed: %s has format errors; run `%s check %s`\n", parentPath, name.Binary, parentPath)
		return exitProblems
	}
	// A parent without a kalka_id gets one first, as on a first send.
	if parent.KalkaID() == "" {
		parent.SetFrontmatter(name.FrontmatterID, bp.NewKalkaID())
		if parent.Owner() == "" {
			parent.SetFrontmatter(name.FrontmatterOwner, email)
		}
		if err := fsutil.WriteAtomic(parentPath, parent.Bytes(), raw); err != nil {
			fmt.Fprintln(stderr, "seed:", err)
			return exitProblems
		}
		if raw, err = os.ReadFile(parentPath); err == nil {
			parent, err = bp.Parse(raw)
		}
		if err != nil {
			fmt.Fprintln(stderr, "seed:", err)
			return exitProblems
		}
	}
	seed, _, err := bp.Bud(parent, bp.SeedSpec{KalkaID: bp.NewKalkaID(), Owner: email, Title: *title, Problem: *msg, Why: *why, At: now(), From: *from}, sign)
	if err != nil {
		fmt.Fprintln(stderr, "seed:", err)
		return exitProblems
	}
	seedPath, err := createBeside(parentPath, store.SafeName(seed.Title()), seed.Bytes())
	if err != nil {
		fmt.Fprintln(stderr, "seed:", err)
		return exitProblems
	}
	// The backlink: re-read and redo if the parent changed meanwhile.
	var entry *bp.Entry
	for attempt := 0; ; attempt++ {
		cur, err := os.ReadFile(parentPath)
		if err == nil {
			var p *bp.Document
			if p, err = bp.Parse(cur); err == nil {
				if entry, err = bp.AddBacklink(p, seed, email, now(), sign); err == nil && entry != nil {
					err = fsutil.WriteAtomic(parentPath, p.Bytes(), cur)
				}
			}
		}
		if errors.Is(err, fsutil.ErrChanged) && attempt < 3 {
			continue
		}
		if err != nil {
			// Never bud a second seed: the backlink is added later, from
			// the seed, in the UI.
			fmt.Fprintf(stderr, "seed: wrote %s, but could not add the backlink to %s: %v\n", seedPath, parentPath, err)
			fmt.Fprintln(stderr, "seed: do not run this again; open the seed in the UI and choose \"Add the backlink\"")
			return exitProblems
		}
		break
	}
	if *asJSON {
		writeJSON(stdout, map[string]string{"seed": seedPath, "seed_id": seed.KalkaID(), "parent": parentPath, "entry": entry.ID})
	} else {
		fmt.Fprintf(stdout, "budded %s (%s) from %s\n", seedPath, seed.KalkaID(), parentPath)
	}
	return exitOK
}

// createBeside writes data to a new file named base.md (or base-N.md)
// next to sibling, never overwriting.
func createBeside(sibling, base string, data []byte) (string, error) {
	dir := filepath.Dir(sibling)
	for i := 0; i < 1000; i++ {
		n := base + ".md"
		if i > 0 {
			n = fmt.Sprintf("%s-%d.md", base, i)
		}
		p := filepath.Join(dir, n)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return p, err
	}
	return "", errors.New("no free file name")
}

type seedRow struct {
	Path        string `json:"path"`
	KalkaID     string `json:"kalka_id"`
	Title       string `json:"title"`
	Owner       string `json:"owner"`
	Parent      string `json:"parent,omitempty"`
	ParentTitle string `json:"parent_title,omitempty"`
	Created     string `json:"created,omitempty"`
	Active      string `json:"active,omitempty"`
}

// `kalka seeds [--json]`: the pre-backlog of the configured folder.
func cmdSeeds(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	fs := flag.NewFlagSet("seeds", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "machine-readable output")
	if _, err := parseInterspersed(fs, args); err != nil {
		return exitUsage
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, "seeds:", err)
		return exitProblems
	}
	st, err := store.Open(cfg.Folder)
	if err != nil {
		fmt.Fprintln(stderr, "seeds:", err)
		return exitProblems
	}
	titles := map[string]string{}
	var rows []seedRow
	for _, info := range st.List() {
		if info.KalkaID != "" {
			titles[info.KalkaID] = info.Title
		}
		f, err := st.Load(info.Path)
		if err != nil || !f.Doc.IsSeed() {
			continue
		}
		r := seedRow{Path: info.Rel, KalkaID: f.Doc.KalkaID(), Title: f.Doc.Title(), Owner: f.Doc.Owner(), Parent: f.Doc.Parent()}
		if c, ok := f.Doc.Created(); ok {
			active := c.UTC()
			for _, it := range f.Doc.Items() {
				if t, err := time.Parse(bp.TimeLayout, it.Entry.Timestamp); err == nil && t.After(active) {
					active = t
				}
			}
			r.Created, r.Active = c.UTC().Format(time.RFC3339), active.Format(time.RFC3339)
		}
		rows = append(rows, r)
	}
	for i := range rows {
		rows[i].ParentTitle = titles[rows[i].Parent]
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Active > rows[j].Active })
	if *asJSON {
		if rows == nil {
			rows = []seedRow{}
		}
		writeJSON(stdout, rows)
		return exitOK
	}
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "no seeds")
	}
	for _, r := range rows {
		age := "age unknown"
		if t, err := time.Parse(time.RFC3339, r.Active); err == nil {
			age = humanAge(now().Sub(t))
		}
		parent := r.ParentTitle
		if parent == "" && r.Parent != "" {
			parent = r.Parent + " (not held)"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\tfrom: %s\t%s\n", r.Title, r.Owner, age, parent, r.Path)
	}
	return exitOK
}

func humanAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return "active now"
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
