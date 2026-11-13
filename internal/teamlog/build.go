package teamlog

import (
	"crypto/ed25519"
	"time"
)

// Signer makes signed operations for one key in one team.
type Signer struct {
	Key  ed25519.PrivateKey
	Team string
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Create returns a team's create-team operation; its hash is the team's
// identity (spec 19.2).
func Create(key ed25519.PrivateKey, teamName, email string, at time.Time) Op {
	o := Op{V: RuleVersion, Kind: CreateTeam, Args: Args{Name: teamName, Email: email}, At: stamp(at), Prev: []string{}}
	o.Sign(key)
	return o
}

func (s Signer) op(kind string, a Args, prev []string, at time.Time) Op {
	o := Op{V: RuleVersion, Team: s.Team, Kind: kind, Args: a, Prev: prev, At: stamp(at)}
	o.Sign(s.Key)
	return o
}

// AddMember binds email to pubkey; invite is the invite id, or empty.
func (s Signer) AddMember(prev []string, email string, pubkey ed25519.PublicKey, invite string, at time.Time) Op {
	return s.op(AddMember, Args{Email: email, PubKey: EncodeKey(pubkey), Invite: invite}, prev, at)
}

// AddOwner makes a member an owner.
func (s Signer) AddOwner(prev []string, email string, at time.Time) Op {
	return s.op(AddOwner, Args{Email: email}, prev, at)
}

// RemoveOwner makes an owner a plain member.
func (s Signer) RemoveOwner(prev []string, email string, at time.Time) Op {
	return s.op(RemoveOwner, Args{Email: email}, prev, at)
}

// AssignDomain makes owner the domain owner for releases above after.
func (s Signer) AssignDomain(prev []string, domain, owner string, after int64, at time.Time) Op {
	return s.op(AssignDomain, Args{Domain: domain, Owner: owner, After: after}, prev, at)
}

// Reassign moves a document whose owner was removed (spec 19.5).
func (s Signer) Reassign(prev []string, doc, from, owner string, after int64, at time.Time) Op {
	return s.op(ReassignDocument, Args{Doc: doc, From: from, Owner: owner, After: after}, prev, at)
}

// Owned is a published document held by the remover whose effective owner
// is the member being removed, with the last release version seen.
type Owned struct {
	Doc   string
	After int64
}

// Remove returns a remove-member followed by one reassign-document per
// document in docs, each giving it to newOwner and naming the removal as
// its only predecessor, so it falls if the removal does (spec 19.5).
func (s Signer) Remove(prev []string, email string, docs []Owned, newOwner string, at time.Time) []Op {
	rm := s.op(RemoveMember, Args{Email: email}, prev, at)
	out := []Op{rm}
	h := rm.Hash()
	for _, d := range docs {
		out = append(out, s.Reassign([]string{h}, d.Doc, email, newOwner, d.After, at))
	}
	return out
}

// Heads returns the hashes of ops that no op in the set names in prev.
func Heads(ops []Op) []string {
	named := map[string]bool{}
	for _, o := range ops {
		for _, p := range o.Prev {
			named[p] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, o := range ops {
		h := o.Hash()
		if !named[h] && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return normPrev(out)
}
