package membership

import (
	"errors"
	"fmt"
)

// ErrPolicyNotBuilt is for group kinds that are designed for but not built.
var ErrPolicyNotBuilt = errors.New("this kind of group is not built yet")

// Policy decides who may do what in a group. Each method is asked about the group as it stood in the entry's
// causal past, so every node gives the same answer.
type Policy interface {
	Mode() Mode
	CanJoin(past *State, author, inviter string) error
	CanRemove(past *State, author, subject string) error
	CanSetRole(past *State, author, subject string) error
	CanSetNodeRoles(past *State, author, subject string) error
	CanRotateKey(past *State, author, subject string) error
	CanRevokeInvite(past *State, author string) error
}

// PolicyFor returns the rules for a group kind.
func PolicyFor(m Mode) (Policy, error) {
	switch m {
	case ModeAuthority:
		return authorityPolicy{}, nil
	case ModeOpen:
		return openPolicy{}, nil
	case ModeKofN:
		return nil, ErrPolicyNotBuilt
	}
	return nil, fmt.Errorf("unknown kind of group %q", m)
}

// ---- managed group: only the authority adds, removes and changes roles ----

type authorityPolicy struct{}

func (authorityPolicy) Mode() Mode { return ModeAuthority }

func onlyAuthority(past *State, author, what string) error {
	if author != past.Authority {
		return fmt.Errorf("only the authority may %s in a managed group", what)
	}
	return nil
}

func (authorityPolicy) CanJoin(past *State, author, _ string) error {
	return onlyAuthority(past, author, "add members")
}

func (authorityPolicy) CanRemove(past *State, author, subject string) error {
	if err := onlyAuthority(past, author, "remove members"); err != nil {
		return err
	}
	if subject == past.Authority {
		return errors.New("the authority cannot be removed")
	}
	return nil
}

func (authorityPolicy) CanSetRole(past *State, author, _ string) error {
	return onlyAuthority(past, author, "change roles")
}

func (authorityPolicy) CanSetNodeRoles(past *State, author, _ string) error {
	return onlyAuthority(past, author, "change what a member does")
}

// A member changes its own key. The authority also does it for members that have no signing key to do it with.
func (authorityPolicy) CanRotateKey(past *State, author, subject string) error {
	if author == subject {
		return nil
	}
	if author == past.Authority && past.Members[subject] != nil && past.Members[subject].SigningPub == "" {
		return nil
	}
	return errors.New("a member may change only its own key")
}

func (authorityPolicy) CanRevokeInvite(past *State, author string) error {
	return onlyAuthority(past, author, "cancel invites")
}

// ---- open group: any member adds and removes; admins change roles ----

type openPolicy struct{}

func (openPolicy) Mode() Mode { return ModeOpen }

func (openPolicy) CanJoin(past *State, author, inviter string) error {
	if !past.Active(inviter) {
		return errors.New("the inviter was not a member of the group")
	}
	return nil // the author being a member is checked for every entry
}

func (openPolicy) CanRemove(*State, string, string) error { return nil } // anyone may remove anyone (decided 2026-10-29)

func isAdmin(past *State, id string) bool {
	m := past.Members[id]
	return m != nil && !m.Removed && m.Admin
}

func (openPolicy) CanSetRole(past *State, author, _ string) error {
	if !isAdmin(past, author) {
		return errors.New("only an admin may change roles")
	}
	return nil
}

func (openPolicy) CanSetNodeRoles(past *State, author, subject string) error {
	if author != subject && !isAdmin(past, author) {
		return errors.New("only an admin may change what another member does")
	}
	return nil
}

func (openPolicy) CanRotateKey(_ *State, author, subject string) error {
	if author != subject {
		return errors.New("a member may change only its own key")
	}
	return nil
}

func (openPolicy) CanRevokeInvite(past *State, author string) error {
	if !isAdmin(past, author) {
		return errors.New("only an admin may cancel invites")
	}
	return nil
}
