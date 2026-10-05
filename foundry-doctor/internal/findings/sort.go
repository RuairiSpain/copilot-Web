package findings

import (
	"cmp"
	"slices"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Sort orders findings in place by file, line, column, rule ID, key and fingerprint. Remaining
// ties are broken by resource identity and evidence text, so equal inputs always give one order.
func Sort(fs []sdk.Finding) {
	slices.SortStableFunc(fs, compareFindings)
}

func compareFindings(a, b sdk.Finding) int {
	return cmp.Or(
		cmp.Compare(a.Location.File, b.Location.File),
		cmp.Compare(a.Location.Line, b.Location.Line),
		cmp.Compare(a.Location.Column, b.Location.Column),
		cmp.Compare(a.RuleID, b.RuleID),
		cmp.Compare(a.Key, b.Key),
		cmp.Compare(a.Fingerprint, b.Fingerprint),
		cmp.Compare(a.Resource.Kind, b.Resource.Kind),
		cmp.Compare(a.Resource.Type, b.Resource.Type),
		cmp.Compare(a.Resource.Name, b.Resource.Name),
		cmp.Compare(a.Resource.Pointer, b.Resource.Pointer),
		cmp.Compare(a.Evidence, b.Evidence),
	)
}
