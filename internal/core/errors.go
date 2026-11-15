package core

import (
	"errors"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/store"
)

// Stable error kinds (spec 12.3), carried by the API and `--json`.
const (
	KindNotMember    = "not-member"
	KindNotTeamOwner = "not-team-owner"
	KindNotOwner     = "not-owner"
	KindInPersonal   = "in-personal"
	KindNotShared    = "not-shared"
	KindDisputed     = "disputed"
	KindDomainExists = "domain-exists"

	KindTransferPending = "transfer-pending"
	KindTransferDecided = "transfer-decided"
	KindUnknownTransfer = "unknown-transfer"

	KindNoTeam        = "no-team"
	KindUnknownTeam   = "unknown-team"
	KindAmbiguousTeam = "ambiguous-team"
	KindLastOwner     = "last-owner"
	KindPaused        = "paused"
	KindFormat        = "format"
	KindDuplicate     = "duplicate"
	// KindError is every other failure: the message says what, and no
	// caller acts on the difference.
	KindError = "error"

	// Reaching kalka, not a team operation: the CLI and the shell report
	// these.
	KindNotSetUp   = "not-set-up"
	KindNotRunning = "not-running"
	KindNoShell    = "no-shell"

	KindInviteToken        = "invite-token"
	KindInviteUsed         = "invite-used"
	KindInviteExpired      = "invite-expired"
	KindInviteEmail        = "invite-email"
	KindRendezvousMismatch = "rendezvous-mismatch"
)

// Error is a failure with a stable kind.
type Error struct {
	Kind string
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// KindOf returns the stable kind of err (spec 12.3): KindError for a
// failure with no kind of its own, "" for nil.
func KindOf(err error) string {
	var (
		e   *Error
		dup *store.ErrDuplicate
	)
	switch {
	case err == nil:
		return ""
	case errors.As(err, &e):
		return e.Kind
	case errors.As(err, &dup):
		return KindDuplicate
	case errors.Is(err, bp.ErrTooLarge), errors.Is(err, bp.ErrNotUTF8), errors.Is(err, bp.ErrOpenBlock):
		return KindFormat
	}
	return KindError
}
