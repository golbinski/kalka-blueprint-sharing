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

// Reassign gives a document to owner from release after+1 on (spec 19.5);
// after and plan name the highest release the signer holds and its plan
// hash (0 and "" before the first release), from the owner it saw.
func (s Signer) Reassign(prev []string, doc, from, owner string, after int64, plan string, at time.Time) Op {
	return s.op(ReassignDocument, Args{Doc: doc, From: from, Owner: owner, After: after, Plan: plan}, prev, at)
}

// Owned is a published document held by the remover whose effective owner
// is the member being removed, with the last release seen.
type Owned struct {
	Doc   string
	After int64
	Plan  string
}

// Remove returns a remove-member followed by one reassign-document per
// document in docs, each giving it to newOwner. They name the removal as
// their predecessor for the history; they do not depend on it (rule 9),
// and fall with the remover's term if the remover loses (rule 2).
func (s Signer) Remove(prev []string, email string, docs []Owned, newOwner string, at time.Time) []Op {
	rm := s.op(RemoveMember, Args{Email: email}, prev, at)
	out := []Op{rm}
	h := rm.Hash()
	for _, d := range docs {
		out = append(out, s.Reassign([]string{h}, d.Doc, email, newOwner, d.After, d.Plan, at))
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
