package transport

import (
	"errors"
	"strings"
	"testing"
)

// A session runs at the lower of the two highest versions, and fails only
// below either side's lowest. A hello without proto_min means proto.
func TestNegotiate(t *testing.T) {
	for _, c := range []struct {
		min, max, want int
		newer          bool
	}{
		{0, 1, 1, false},
		{1, 1, 1, false},
		{1, 3, 1, false}, // a newer peer that still speaks 1
		{2, 3, 0, true},  // a newer peer that no longer does
		{0, 0, 0, false},
		{3, 2, 0, false},
	} {
		v, err := Negotiate(c.min, c.max)
		if v != c.want || (err == nil) != (c.want != 0) {
			t.Errorf("Negotiate(%d, %d) = %d, %v", c.min, c.max, v, err)
			continue
		}
		var pe *ProtoError
		if err != nil && (!errors.As(err, &pe) || strings.Contains(err.Error(), "newer") != c.newer) {
			t.Errorf("Negotiate(%d, %d): %v", c.min, c.max, err)
		}
	}
}
