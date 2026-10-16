package foundrylink

import (
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Ownership says whether an account owns child resources.
type Ownership int

// Ownership values.
const (
	OwnsNone Ownership = iota
	// OwnsYes: a deployed child provably belongs to the account.
	OwnsYes
	// OwnsMaybe: a child might belong to the account, but it is conditional or
	// its parent name cannot be correlated.
	OwnsMaybe
)

// Owns reports whether account owns a child resource of one of the given types
// (for example accounts/projects or accounts/deployments). The parent name
// segments of the child are compared with the account name. When they cannot
// be correlated and the template has exactly one account, that account is
// assumed to be the parent. A conditional child proves nothing (OwnsMaybe).
func Owns(m sdk.ARMModel, account sdk.ARMResource, accounts int, types ...string) Ownership {
	self := armexpr.ParseName(account.Type, account.Name)
	res := OwnsNone
	for _, c := range m.Resources() {
		if !hasType(c.Type, types) {
			continue
		}
		pr := armexpr.PresenceOf(c.Conditional, c.Condition, c.Looped)
		if pr == armexpr.NotDeployed {
			continue
		}
		segs := armexpr.ParseName(c.Type, c.Name).Segments
		cmp := armexpr.Unknown
		if len(segs) >= 2 {
			cmp = armexpr.Compare(armexpr.Ref{Type: self.Type, Segments: segs[:len(segs)-1]}, self)
		}
		if cmp == armexpr.Unknown && accounts == 1 {
			cmp = armexpr.Same
		}
		switch cmp {
		case armexpr.Same:
			if pr == armexpr.Deployed {
				return OwnsYes
			}
			res = OwnsMaybe
		case armexpr.Unknown:
			res = OwnsMaybe
		}
	}
	return res
}

func hasType(t string, types []string) bool {
	for _, x := range types {
		if strings.EqualFold(t, x) {
			return true
		}
	}
	return false
}
