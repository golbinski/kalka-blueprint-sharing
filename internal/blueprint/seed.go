package blueprint

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/golbinski/kalka/internal/name"
)

// Frontmatter keys of seeds (spec 6.11) and of the document graph (spec
// 6.18).
const (
	KeyStatus    = name.Product + "_status"
	KeyParent    = name.Product + "_parent"
	KeyOrigin    = name.Product + "_origin"
	KeyCreated   = name.Product + "_created"
	KeyBreakdown = name.Product + "_breakdown"

	StatusSeed = "seed"

	BreakdownDecomposed = "decomposed"
	BreakdownAdopted    = "adopted"
)

func (d *Document) get(key string) string {
	v, _ := d.fm.Get(key)
	return strings.TrimSpace(v)
}

// Status is the `kalka_status:` key, or "".
func (d *Document) Status() string { return d.get(KeyStatus) }

// IsSeed reports `kalka_status: seed`.
func (d *Document) IsSeed() bool { return d.Status() == StatusSeed }

var (
	uuidRe       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	seedOriginRe = regexp.MustCompile(`^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(?:#([0-9a-f]{32}))?$`)
)

// Origin is where a seed came from (spec 6.11): the kalka_id of the
// document it was split from and, when it started from an entry, that
// entry's id. ok is false when kalka_origin is absent or malformed.
func (d *Document) Origin() (doc, entry string, ok bool) {
	m := seedOriginRe.FindStringSubmatch(strings.ToLower(d.get(KeyOrigin)))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// OriginDoc is the kalka_id of the document a seed was split from, or "".
func (d *Document) OriginDoc() string {
	doc, _, _ := d.Origin()
	return doc
}

// PlanParent is the kalka_id of the parent plan node a document that is
// not a domain document names with kalka_parent (spec 6.18), or "".
func (d *Document) PlanParent() string {
	if d.IsDomain() {
		return ""
	}
	if v := strings.ToLower(d.get(KeyParent)); uuidRe.MatchString(v) {
		return v
	}
	return ""
}

// Breakdown is kalka_breakdown: decomposed, adopted, or "".
func (d *Document) Breakdown() string {
	switch v := d.get(KeyBreakdown); v {
	case BreakdownDecomposed, BreakdownAdopted:
		return v
	}
	return ""
}

// Created is the `kalka_created:` time; ok is false if absent or unreadable.
func (d *Document) Created() (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, d.get(KeyCreated))
	return t, err == nil
}

// SeedSpec is what budding needs (spec 6.11).
type SeedSpec struct {
	KalkaID string // the seed's new kalka_id
	Owner   string // the budding user
	Title   string
	Problem string
	Why     string
	At      time.Time
	// From is the id of the parent entry the seed starts from, or "".
	From string
}

var budBodyRe = regexp.MustCompile(`^budded: ([0-9a-f-]{36})$`)

// Bud splits a seed off parent: it returns the seed document and the
// signed budded entry, already appended to parent. Parent's frontmatter
// and body are not touched. Parent must have a kalka_id.
func Bud(parent *Document, spec SeedSpec, sign Signer) (*Document, *Entry, error) {
	pid := parent.KalkaID()
	if pid == "" {
		return nil, nil, errors.New("the parent needs a kalka_id first")
	}
	title := strings.TrimSpace(strings.ReplaceAll(spec.Title, "\n", " "))
	if title == "" {
		return nil, nil, errors.New("a seed needs a title")
	}
	if !emailRe.MatchString(spec.Owner) {
		return nil, nil, fmt.Errorf("owner %q is not an email address", spec.Owner)
	}
	anchor, re := "", ""
	var origin *Entry
	if spec.From != "" {
		for _, it := range parent.Items() {
			if it.Entry.ID == spec.From {
				origin = it.Entry
			}
		}
		if origin == nil {
			return nil, nil, fmt.Errorf("no entry with id %q in the parent", spec.From)
		}
		re = origin.ID
		if origin.Anchor != "" && parent.HasAnchor(origin.Anchor) {
			anchor = origin.Anchor
		}
	}
	problem := strings.TrimSpace(spec.Problem)
	if problem == "" && origin != nil {
		problem = quote(displayText(origin)) + "\n\n— " + origin.Kind + " by " + origin.Author + " on " + origin.Timestamp
	}
	if problem == "" {
		return nil, nil, errors.New("describe the problem the seed is about")
	}
	why := strings.TrimSpace(spec.Why)
	if why == "" {
		why = "Split off from \"" + parent.Title() + "\" to keep its scope."
	}
	at := spec.At.UTC().Format(time.RFC3339)
	var b strings.Builder
	from := pid
	if origin != nil {
		from += "#" + origin.ID
	}
	fmt.Fprintf(&b, "---\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n", name.FrontmatterID, spec.KalkaID, name.FrontmatterOwner, spec.Owner, KeyStatus, StatusSeed, KeyOrigin, from)
	fmt.Fprintf(&b, "%s: %s\n---\n\n# %s\n\n## Problem\n\n%s\n\n## Why it was split off\n\n%s\n", KeyCreated, at, title, problem, why)
	seed, err := Parse([]byte(b.String()))
	if err != nil {
		return nil, nil, err
	}
	if seed.HasErrors() {
		return nil, nil, fmt.Errorf("the seed would be malformed: %s", seed.Problems()[0].Message)
	}
	e, err := Append(parent, KindBudded, spec.Owner, spec.At, anchor, re, "", "budded: "+spec.KalkaID+"\n"+title, sign)
	if err != nil {
		return nil, nil, err
	}
	return seed, e, nil
}

// AddBacklink appends the budded entry for an existing seed to parent,
// unless parent already has one for it: the retry of Bud's second write.
// It returns nil, nil when the backlink is already there.
func AddBacklink(parent, seed *Document, author string, at time.Time, sign Signer) (*Entry, error) {
	if seed.OriginDoc() != parent.KalkaID() {
		return nil, errors.New("this seed was not split off from this blueprint")
	}
	for _, it := range parent.Items() {
		if id, _, ok := it.Entry.Budded(); ok && id == seed.KalkaID() {
			return nil, nil
		}
	}
	anchor, re := "", ""
	for _, it := range parent.Items() {
		if _, o, _ := seed.Origin(); o != "" && it.Entry.ID == o {
			re = o
			if it.Entry.Anchor != "" && parent.HasAnchor(it.Entry.Anchor) {
				anchor = it.Entry.Anchor
			}
		}
	}
	return Append(parent, KindBudded, author, at, anchor, re, "", "budded: "+seed.KalkaID()+"\n"+seed.Title(), sign)
}

// Budded parses a budded entry: the seed's kalka_id and title.
func (e *Entry) Budded() (seedID, title string, ok bool) {
	if e.Kind != KindBudded {
		return "", "", false
	}
	lines := strings.Split(Normalize(e.Body), "\n")
	if len(lines) > 0 && e.ReplyTo() != "" {
		lines = lines[1:]
	}
	if len(lines) == 0 {
		return "", "", false
	}
	m := budBodyRe.FindStringSubmatch(strings.TrimSpace(lines[0]))
	if m == nil {
		return "", "", false
	}
	return m[1], strings.TrimSpace(strings.Join(lines[1:], " ")), true
}

func displayText(e *Entry) string {
	b := Normalize(e.Body)
	if e.ReplyTo() != "" {
		_, b, _ = strings.Cut(b, "\n")
	}
	return strings.TrimSpace(b)
}

func quote(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+l, " ")
	}
	return strings.Join(lines, "\n")
}

// Promote sets status to a value other than seed (spec 6.11). Only the
// owner may; the caller checks.
func Promote(d *Document, status string) error {
	status = strings.TrimSpace(status)
	if status == "" || status == StatusSeed || strings.ContainsAny(status, ":\n#") {
		return fmt.Errorf("status %q: choose another status than seed, such as draft", status)
	}
	if !d.IsSeed() {
		return errors.New("this blueprint is not a seed")
	}
	d.SetFrontmatter(KeyStatus, status)
	return nil
}

// NewKalkaID returns a fresh UUID v4 (spec 6.2).
func NewKalkaID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// graphProblems checks the shape of kalka_parent, kalka_origin and
// kalka_breakdown in a document that is not a domain document (spec 6.2).
func (d *Document) graphProblems() []Problem {
	var out []Problem
	if raw := d.get(KeyParent); raw != "" && !d.IsDomain() && d.PlanParent() == "" {
		out = append(out, NewProblem("F014", d.fm.KeyLine(KeyParent), fmt.Sprintf("kalka_parent names the parent plan node by its kalka_id, a UUID (have %q); it is ignored", raw)))
	}
	if raw := d.get(KeyOrigin); raw != "" {
		if _, _, ok := d.Origin(); !ok {
			out = append(out, NewProblem("F015", d.fm.KeyLine(KeyOrigin), fmt.Sprintf("kalka_origin is <kalka_id> or <kalka_id>#<entry id> (have %q); it is ignored", raw)))
		}
	}
	if raw := d.get(KeyBreakdown); raw != "" && d.Breakdown() == "" {
		out = append(out, NewProblem("F016", d.fm.KeyLine(KeyBreakdown), fmt.Sprintf("kalka_breakdown is decomposed or adopted (have %q); it is ignored", raw)))
	}
	return out
}
