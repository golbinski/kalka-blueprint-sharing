package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
)

// BudRequest creates a seed from a blueprint (spec 6.11).
type BudRequest struct {
	Path    string `json:"path"` // the parent
	From    string `json:"from,omitempty"`
	Title   string `json:"title"`
	Problem string `json:"problem,omitempty"`
	Why     string `json:"why,omitempty"`
}

// BudResult says what was written. When the backlink could not be added,
// Seed is set and BacklinkError says why; retry with Backlink, never by
// budding again.
type BudResult struct {
	Seed          string `json:"seed"` // path relative to the folder
	SeedID        string `json:"seed_id"`
	Entry         string `json:"entry,omitempty"`
	BacklinkError string `json:"backlink_error,omitempty"`
}

// ErrBacklink wraps a failed backlink write after the seed was created.
var ErrBacklink = errors.New("the seed was created, but the backlink could not be added to the parent")

// Bud writes a seed beside its parent and appends the signed budded
// entry to the parent. A parent without a kalka_id gets one first.
func (d *Node) Bud(req BudRequest) (BudResult, error) {
	var res BudResult
	path := d.resolve(req.Path)
	f, err := d.Store.Load(path)
	if err != nil {
		return res, err
	}
	if f.Doc.HasErrors() {
		return res, fmt.Errorf("%s has format errors", f.Path)
	}
	if f.Doc.KalkaID() == "" {
		if f, err = d.prepare(f); err != nil {
			return res, err
		}
	}
	unlock, ok := d.Store.Lock(f.Doc.KalkaID(), 10*time.Second)
	if !ok {
		return res, errors.New("the blueprint is busy in a sync; try again")
	}
	defer unlock()
	if f, err = d.Store.Load(f.Path); err != nil {
		return res, err
	}
	parent, err := bp.Parse(f.Raw)
	if err != nil {
		return res, err
	}
	spec := bp.SeedSpec{KalkaID: bp.NewKalkaID(), Owner: d.self.Email, Title: req.Title, Problem: req.Problem, Why: req.Why, At: d.now(), From: req.From}
	seed, _, err := bp.Bud(parent, spec, d.signer())
	if err != nil {
		return res, err
	}
	seedPath, err := d.Store.CreateIn(filepath.Dir(f.Path), seed, seed.Title())
	if err != nil {
		return res, err
	}
	res.SeedID = seed.KalkaID()
	res.Seed = d.rel(seedPath)
	d.logf("budded seed %s from %s", seedPath, f.Path)
	// A seed of a published parent is published to the same team (6.11).
	if t := f.Doc.Team(); t != "" && bp.SameEmail(seed.Owner(), d.self.Email) {
		if _, err := d.heldByName(t); err == nil {
			if _, err := d.Publish(seedPath, t, "split off "+f.Doc.Title()); err != nil {
				d.logf("publishing seed %s: %v", seedPath, err)
			}
		}
	}
	// The second write: the parent with its budded entry. If the file
	// changed meanwhile, the entry is added again to the fresh copy.
	e, err := d.backlink(f.Path, seed)
	if err != nil {
		res.BacklinkError = err.Error()
		return res, fmt.Errorf("%w: %v", ErrBacklink, err)
	}
	if e != nil {
		res.Entry = e.ID
	}
	return res, nil
}

// Backlink adds the budded entry for a seed whose parent missed it. It
// does nothing if the parent already has one.
func (d *Node) Backlink(seedPath string) (*bp.Entry, error) {
	sf, err := d.Store.Load(d.resolve(seedPath))
	if err != nil {
		return nil, err
	}
	if sf.Doc.Parent() == "" {
		return nil, errors.New("this document names no parent")
	}
	pf, err := d.Store.Find(sf.Doc.Parent())
	if err != nil {
		return nil, err
	}
	if pf == nil {
		return nil, errors.New("the parent blueprint is not in the folder")
	}
	unlock, ok := d.Store.Lock(pf.Doc.KalkaID(), 10*time.Second)
	if !ok {
		return nil, errors.New("the parent is busy in a sync; try again")
	}
	defer unlock()
	return d.backlink(pf.Path, sf.Doc)
}

func (d *Node) backlink(parentPath string, seed *bp.Document) (*bp.Entry, error) {
	for attempt := 0; ; attempt++ {
		f, err := d.Store.Load(parentPath)
		if err != nil {
			return nil, err
		}
		parent, err := bp.Parse(f.Raw)
		if err != nil {
			return nil, err
		}
		e, err := bp.AddBacklink(parent, seed, d.self.Email, d.now(), d.signer())
		if err != nil || e == nil {
			return nil, err
		}
		err = d.Store.Update(f, parent)
		if errors.Is(err, fsutil.ErrChanged) && attempt < 3 {
			continue
		}
		if err != nil {
			return nil, err
		}
		return e, nil
	}
}

// Promote changes a seed's status; only its owner may (spec 6.11).
func (d *Node) Promote(path, status string) error {
	if status == "" {
		status = "draft"
	}
	for attempt := 0; ; attempt++ {
		f, err := d.Store.Load(d.resolve(path))
		if err != nil {
			return err
		}
		doc, err := bp.Parse(f.Raw)
		if err != nil {
			return err
		}
		if !bp.SameEmail(doc.Owner(), d.self.Email) {
			return fmt.Errorf("only the owner (%s) can promote this seed", doc.Owner())
		}
		if err := bp.Promote(doc, status); err != nil {
			return err
		}
		err = d.Store.Update(f, doc)
		if errors.Is(err, fsutil.ErrChanged) && attempt < 3 {
			continue
		}
		if err == nil {
			d.logf("promoted %s to %s", f.Path, status)
			d.releaseOwn(f.Path, "promoted to "+status)
		}
		return err
	}
}

// SeedView is one row of the pre-backlog.
type SeedView struct {
	Rel         string `json:"rel"`
	KalkaID     string `json:"kalka_id"`
	Title       string `json:"title"`
	Owner       string `json:"owner"`
	Mine        bool   `json:"mine"`
	Parent      string `json:"parent,omitempty"`       // kalka_id
	ParentRel   string `json:"parent_rel,omitempty"`   // when held
	ParentTitle string `json:"parent_title,omitempty"` // when held
	Created     string `json:"created,omitempty"`
	// Active is the later of created and the newest entry; empty when the
	// seed has no created key ("age unknown").
	Active string `json:"active,omitempty"`
}

// Seeds lists every seed in the folder, most recently active first.
func (d *Node) Seeds() []SeedView {
	out := []SeedView{}
	for _, info := range d.Store.List() {
		f, err := d.Store.Load(info.Path)
		if err != nil || !f.Doc.IsSeed() {
			continue
		}
		doc := f.Doc
		v := SeedView{Rel: d.rel(f.Path), KalkaID: doc.KalkaID(), Title: doc.Title(), Owner: doc.Owner(),
			Mine: bp.SameEmail(doc.Owner(), d.self.Email), Parent: doc.Parent()}
		if v.Parent != "" {
			if pf, err := d.Store.Find(v.Parent); err == nil && pf != nil {
				v.ParentRel, v.ParentTitle = d.rel(pf.Path), pf.Doc.Title()
			}
		}
		if c, ok := doc.Created(); ok {
			v.Created = c.UTC().Format(time.RFC3339)
			active := c.UTC()
			for _, it := range doc.Items() {
				if t, err := time.Parse(bp.TimeLayout, it.Entry.Timestamp); err == nil && t.After(active) {
					active = t
				}
			}
			v.Active = active.Format(time.RFC3339)
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active > out[j].Active
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out
}

func (d *Node) rel(p string) string {
	r, err := filepath.Rel(d.Store.Dir(), p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(r)
}
