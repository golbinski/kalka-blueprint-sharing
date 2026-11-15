package core

import "errors"

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
)

// Error is a failure with a stable kind.
type Error struct {
	Kind string
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// KindOf returns the stable kind of err, or "".
func KindOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return ""
}
