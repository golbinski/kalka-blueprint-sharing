package teamlog

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/golbinski/kalka/internal/fsutil"
)

// LogName is the file name of a team's log inside its folder (spec 19.1).
func LogName(teamName string) string { return teamName + "-team.log" }

// BadLine is a line of a log file that does not decode.
type BadLine struct {
	Line int    `json:"line"`
	Err  string `json:"error"`
}

// ReadFile reads a log file: one encoded operation per line. Lines that do
// not decode are reported and skipped; a duplicate line is read once. A
// missing file is an empty log.
func ReadFile(path string) ([]Op, []BadLine, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	ops, bad := parseLines(raw)
	return ops, bad, nil
}

func parseLines(raw []byte) ([]Op, []BadLine) {
	var ops []Op
	var bad []BadLine
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, MaxLine+2), MaxLine+2)
	n := 0
	for sc.Scan() {
		n++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		o, err := Decode(line)
		if err != nil {
			bad = append(bad, BadLine{n, err.Error()})
			continue
		}
		if h := o.Hash(); !seen[h] {
			seen[h] = true
			ops = append(ops, o)
		}
	}
	if err := sc.Err(); err != nil {
		bad = append(bad, BadLine{n + 1, err.Error()})
	}
	return ops, bad
}

// AppendFile adds ops to the log at path, creating it and its folder. The
// file is re-read just before writing and the union is written atomically
// (spec 14), so operations another writer added meanwhile are kept;
// operations already present are not repeated. Lines that do not decode
// are kept as they are: kalka never drops bytes it cannot read. It returns
// the operations the file holds afterwards.
func AppendFile(path string, ops []Op) ([]Op, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		cur, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		have, _ := parseLines(cur)
		known := map[string]bool{}
		for _, o := range have {
			known[o.Hash()] = true
		}
		out := bytes.Clone(cur)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
		added := false
		for _, o := range ops {
			h := o.Hash()
			if known[h] {
				continue
			}
			known[h] = true
			out = append(append(out, o.Encode()...), '\n')
			have = append(have, o)
			added = true
		}
		if !added {
			return have, nil
		}
		var expect []byte
		if cur != nil {
			expect = cur
		}
		err = fsutil.WriteAtomic(path, out, expect)
		if errors.Is(err, fsutil.ErrChanged) && attempt < 5 {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("writing %s: %w", path, err)
		}
		return have, nil
	}
}
