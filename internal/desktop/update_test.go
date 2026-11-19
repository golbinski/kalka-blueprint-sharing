package desktop

import (
	"testing"

	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/daemon"
)

func TestTrayUpdateItem(t *testing.T) {
	base := StartingTray()
	for _, c := range []struct {
		st   daemon.UpdateStatus
		want string
	}{
		{daemon.UpdateStatus{State: daemon.UpdateNone}, ""},
		{daemon.UpdateStatus{State: daemon.UpdateReady, Latest: "0.1.5", CanInstall: true}, "Restart to update to 0.1.5"},
		{daemon.UpdateStatus{State: daemon.UpdateDownloading, Latest: "0.1.5"}, "Downloading 0.1.5…"},
		{daemon.UpdateStatus{State: daemon.UpdateAvailable, Latest: "0.2.0", CanInstall: true}, "Download 0.2.0 and restart"},
		{daemon.UpdateStatus{State: daemon.UpdateAvailable, Latest: "0.2.0"}, "kalka 0.2.0 is available"},
	} {
		if got := base.WithUpdate(c.st).Update; got != c.want {
			t.Errorf("%+v: %q, want %q", c.st, got, c.want)
		}
	}
	if h := NoticeHash(core.Notice{Kind: core.NoticeUpdate}); h != "#/settings" {
		t.Errorf("update notice opens %q", h)
	}
}
