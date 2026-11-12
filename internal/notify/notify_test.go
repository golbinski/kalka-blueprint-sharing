package notify

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// TestPopupCloses shows a real popup and checks it closes when ctx ends.
// Opt-in: it puts a window on the screen. KALKA_POPUP_TEST=1.
func TestPopupCloses(t *testing.T) {
	if os.Getenv("KALKA_POPUP_TEST") == "" {
		t.Skip("set KALKA_POPUP_TEST=1 to show a real popup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	ok, err := Ask(ctx, `kalka: test "quotes" & \ backslash`, "alice@corp.com sends you a new blueprint: “end run\" & do shell script \"touch /tmp/pwned”.\n\nThis test popup closes by itself.")
	if ok || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("popup closed late: %v", d)
	}
	if _, err := os.Stat("/tmp/pwned"); err == nil {
		t.Fatal("message text ran as script")
	}
}
