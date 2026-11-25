package core

import (
	"errors"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/plugin"
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
	// KindNoApproval: seeds, domain documents and tombstones have no
	// approval state (spec 6.18).
	KindNoApproval = "no-approval"

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

	// KindRendezvousSilent: a server to switch to did not answer; forcing
	// switches anyway.
	KindRendezvousSilent = "rendezvous-silent"

	// Plugin settings (spec 10.6).
	KindNoPlugin         = "no-plugin"
	KindNoPluginSettings = "no-plugin-settings"
	KindPluginValue      = "plugin-value"
	KindPluginRejected   = "plugin-rejected"
	KindPluginFailed     = "plugin-failed"
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
		ve  *plugin.ValueError
		rej *plugin.RejectedError
		pf  *plugin.FailedError
	)
	switch {
	case err == nil:
		return ""
	case errors.As(err, &e):
		return e.Kind
	case errors.Is(err, plugin.ErrNotFound):
		return KindNoPlugin
	case errors.Is(err, plugin.ErrNoSettings), errors.Is(err, plugin.ErrIncomplete):
		return KindNoPluginSettings
	case errors.As(err, &ve):
		return KindPluginValue
	case errors.As(err, &rej):
		return KindPluginRejected
	case errors.As(err, &pf):
		return KindPluginFailed
	case errors.As(err, &dup):
		return KindDuplicate
	case errors.Is(err, bp.ErrTooLarge), errors.Is(err, bp.ErrNotUTF8), errors.Is(err, bp.ErrOpenBlock):
		return KindFormat
	}
	return KindError
}

// FieldsOf returns the messages per field of a plugin-value or
// plugin-rejected error, or nil.
func FieldsOf(err error) map[string]string {
	var (
		ve  *plugin.ValueError
		rej *plugin.RejectedError
	)
	switch {
	case errors.As(err, &ve):
		return ve.Fields
	case errors.As(err, &rej):
		return rej.Fields
	}
	return nil
}
