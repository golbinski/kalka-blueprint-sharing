package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The project's releases.
const (
	DefaultAPI  = "https://api.github.com"
	DefaultRepo = "golbinski/kalka-blueprint-sharing"
)

// SumsAsset is the checksum file every release has (spec 15).
const SumsAsset = "SHA256SUMS"

// maxAsset bounds a download.
const maxAsset = 500 << 20

// Source is where releases are listed; tests point API at a local server.
type Source struct {
	API  string // default DefaultAPI
	Repo string // owner/name, default DefaultRepo
	HTTP *http.Client
	// Keys are the public keys a release's SHA256SUMS may be signed with,
	// default ReleaseKeys.
	Keys []string
}

// Release is one published release.
type Release struct {
	Version  Version
	Tag      string
	NotesURL string
	Assets   map[string]string // name -> download URL
}

func (s Source) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (s Source) get(ctx context.Context, url, etag string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "kalka-updater")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	return s.client().Do(req)
}

// Releases lists the published releases, drafts left out. With the etag of
// an earlier answer, notModified reports that nothing changed.
func (s Source) Releases(ctx context.Context, etag string) (rels []Release, newETag string, notModified bool, err error) {
	api, repo := s.API, s.Repo
	if api == "" {
		api = DefaultAPI
	}
	if repo == "" {
		repo = DefaultRepo
	}
	resp, err := s.get(ctx, strings.TrimSuffix(api, "/")+"/repos/"+repo+"/releases?per_page=30", etag)
	if err != nil {
		return nil, "", false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotModified:
		return nil, etag, true, nil
	case http.StatusOK:
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, "", false, errors.New("GitHub's rate limit is reached; kalka tries again later")
	default:
		return nil, "", false, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var raw []struct {
		Tag    string `json:"tag_name"`
		URL    string `json:"html_url"`
		Draft  bool   `json:"draft"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&raw); err != nil {
		return nil, "", false, fmt.Errorf("GitHub's release list: %w", err)
	}
	for _, r := range raw {
		v, err := Parse(r.Tag)
		if r.Draft || err != nil {
			continue
		}
		rel := Release{Version: v, Tag: r.Tag, NotesURL: r.URL, Assets: map[string]string{}}
		for _, a := range r.Assets {
			rel.Assets[a.Name] = a.URL
		}
		rels = append(rels, rel)
	}
	return rels, resp.Header.Get("ETag"), false, nil
}

// Pick chooses the release to offer to a build at cur: pre-releases count
// only for a pre-release build; the newest compatible release wins, and
// without one the newest release of all (compatible false).
func Pick(rels []Release, cur Version) (best Release, compatible, ok bool) {
	for _, r := range rels {
		if r.Version.Compare(cur) <= 0 || (r.Version.Pre != "" && cur.Pre == "") {
			continue
		}
		c := Compatible(cur, r.Version)
		if !ok || (c && !compatible) || (c == compatible && r.Version.Compare(best.Version) > 0) {
			best, compatible, ok = r, c, true
		}
	}
	return best, compatible, ok
}

// AssetName is the desktop package of a release for a system, or false
// where none is published.
func AssetName(v Version, goos, goarch string) (string, bool) {
	base := fmt.Sprintf("kalka_%s_%s_%s", v, goos, goarch)
	switch goos {
	case "windows":
		return base + "_setup.exe", true
	case "darwin":
		return base + "_app.zip", true
	case "linux":
		return base + "_desktop.tar.gz", true
	}
	return "", false
}

// Sums parses a SHA256SUMS file into name -> hex digest.
func Sums(raw []byte) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(string(raw), "\n") {
		f := strings.Fields(l)
		if len(f) == 2 && len(f[0]) == 64 {
			out[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	return out
}

// Fetch makes sure the release's package for asset is in dir, verified
// against the release's SHA256SUMS, itself signed with a release key and
// naming the release, and returns its path. A package that
// is already there and matches is kept; one that does not match is
// refused and removed.
func (s Source) Fetch(ctx context.Context, rel Release, asset, dir string) (string, error) {
	url, ok := rel.Assets[asset]
	if !ok {
		return "", fmt.Errorf("release %s has no %s", rel.Tag, asset)
	}
	sumsURL, ok := rel.Assets[SumsAsset]
	if !ok {
		return "", fmt.Errorf("release %s has no %s: not installing what cannot be verified", rel.Tag, SumsAsset)
	}
	raw, err := s.body(ctx, sumsURL, 1<<20)
	if err != nil {
		return "", err
	}
	if err := s.verifySums(ctx, rel, raw); err != nil {
		return "", err
	}
	want, ok := Sums(raw)[asset]
	if !ok {
		return "", fmt.Errorf("%s does not list %s", SumsAsset, asset)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, asset)
	if got, err := fileSum(dst); err == nil && got == want {
		return dst, nil
	}
	_ = os.Remove(dst)
	resp, err := s.get(ctx, url, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download of %s: %s", asset, resp.Status)
	}
	part := dst + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxAsset+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxAsset {
		err = errors.New("download too large")
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != want {
		err = fmt.Errorf("%s does not match its checksum in %s; not installing it", asset, SumsAsset)
	}
	if err != nil {
		_ = os.Remove(part)
		return "", err
	}
	return dst, os.Rename(part, dst)
}

// verifySums checks the release's signature over its SHA256SUMS.
func (s Source) verifySums(ctx context.Context, rel Release, sums []byte) error {
	sigURL, ok := rel.Assets[SigAsset]
	if !ok {
		return fmt.Errorf("release %s has no %s: not installing what cannot be verified", rel.Tag, SigAsset)
	}
	sig, err := s.body(ctx, sigURL, 4<<10)
	if err != nil {
		return err
	}
	ks := s.Keys
	if ks == nil {
		ks = ReleaseKeys
	}
	keys, err := releaseKeys(ks)
	if err != nil {
		return err
	}
	trusted, err := Verify(sums, sig, keys)
	if err != nil {
		return fmt.Errorf("%s of release %s: %v; not installing it", SumsAsset, rel.Tag, err)
	}
	if trusted != TrustedComment(rel.Tag) {
		return fmt.Errorf("%s of release %s is signed for %q; not installing it", SumsAsset, rel.Tag, trusted)
	}
	return nil
}

func (s Source) body(ctx context.Context, url string, limit int64) ([]byte, error) {
	resp, err := s.get(ctx, url, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
