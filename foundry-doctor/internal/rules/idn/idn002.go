package idn

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/foundrylink"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// FND-IDN-002 static leg: the Foundry project (or account) managed identity must
// hold the documented data-plane roles on the stores it is linked to. Linkage
// comes from internal/foundrylink (connections, capability hosts and
// capabilitySettings). Required roles and GUIDs are those in the FND-IDN-002
// catalogue entry (verified against the Azure built-in roles reference). Live
// role assignments are out of scope here: they need the preflight or runtime
// check.

const (
	typeSearch   = "Microsoft.Search/searchServices"
	typeStorage  = "Microsoft.Storage/storageAccounts"
	typeCosmos   = "Microsoft.DocumentDB/databaseAccounts"
	typeKeyVault = "Microsoft.KeyVault/vaults"
)

// idn002Capability names the input whose absence skips the rule.
const idn002Capability = "Microsoft.Authorization/roleAssignments in the compiled template (live role assignments need the preflight or runtime check)"

type roleReq struct{ guid, name string }

var (
	roleSearchIndexDataContributor = roleReq{"8ebe5a00-799e-43f5-93ac-243d3dce84a7", "Search Index Data Contributor"}
	roleSearchServiceContributor   = roleReq{"7ca78c08-252a-4471-8644-bb5ff32d4ba0", "Search Service Contributor"}
	roleBlobDataContributor        = roleReq{"ba92f5b4-2d11-453d-a403-e96b0029c9fe", "Storage Blob Data Contributor"}
	roleBlobDataOwner              = roleReq{"b7e6dc6d-f1e8-4753-8033-0f276bb0955b", "Storage Blob Data Owner"}
	roleStorageAccountContributor  = roleReq{"17d1049b-9a84-46fb-8f53-869881c3d3ab", "Storage Account Contributor"}
	roleCosmosOperator             = roleReq{"230815da-be43-4aae-9cb4-875f7bd000aa", "Cosmos DB Operator"}
)

// isCatalogueRole reports whether guid is one of the roles this rule knows by GUID.
func isCatalogueRole(guid string) bool {
	for _, r := range []roleReq{roleSearchIndexDataContributor, roleSearchServiceContributor, roleBlobDataContributor, roleBlobDataOwner, roleStorageAccountContributor, roleCosmosOperator} {
		if strings.EqualFold(r.guid, guid) {
			return true
		}
	}
	return false
}

// requiredRoles are the roles whose absence is a finding, by lower-cased target
// type. Storage Account Contributor is reported only as a note (catalogue
// notes: not confirmed as required), and the Cosmos DB Built-in Data
// Contributor SQL role and any Key Vault role are not verified, so they never
// produce a finding.
func requiredRoles(targetType string) []roleReq {
	switch strings.ToLower(targetType) {
	case strings.ToLower(typeSearch):
		return []roleReq{roleSearchIndexDataContributor, roleSearchServiceContributor}
	case strings.ToLower(typeStorage):
		return []roleReq{roleBlobDataContributor, roleBlobDataOwner}
	case strings.ToLower(typeCosmos):
		return []roleReq{roleCosmosOperator}
	}
	return nil
}

type tri int

const (
	triNo tri = iota
	triYes
	triUnknown
)

// matchPrincipal decides whether a role assignment principalId is the identity
// of owner. A system-assigned principal ID is generated at deployment, so a
// literal GUID can never be it; a compiled reference(...) expression is matched
// to the owner or to one of its user-assigned identities with armexpr.
func matchPrincipal(pid string, owner sdk.ARMResource) tri {
	pid = strings.TrimSpace(pid)
	if pid == "" {
		return triUnknown
	}
	uais := userAssignedKeys(owner)
	if !armexpr.IsExpression(pid) {
		if v, ok, e := strOf(owner.Identity, "principalId"); ok && !e && strings.EqualFold(v, pid) {
			return triYes
		}
		if len(uais) > 0 {
			return triUnknown // the principal of a user-assigned identity may be known up front
		}
		return triNo
	}
	c, ok := armexpr.ParseCall(pid)
	if !ok || c.Fn != "reference" || !strings.Contains(strings.ToLower(c.Suffix), "principalid") {
		return triUnknown
	}
	target := armexpr.ParseRef(pid)
	if target.Opaque || len(target.Segments) == 0 {
		return triUnknown
	}
	res := triNo
	candidates := []armexpr.Ref{armexpr.ParseName(owner.Type, owner.Name)}
	for _, k := range uais {
		candidates = append(candidates, armexpr.ParseRef(k))
	}
	for _, cand := range candidates {
		switch armexpr.Compare(target, cand) {
		case armexpr.Same:
			return triYes
		case armexpr.Unknown:
			res = triUnknown
		}
	}
	return res
}

func userAssignedKeys(r sdk.ARMResource) []string {
	m, ok := r.Identity["userAssignedIdentities"].(map[string]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type cover int

const (
	coverOther   cover = iota // the scope provably does not contain the target
	coverFull                 // the scope is the target or a broad scope containing it
	coverNarrow               // the scope is a child of the target (for example a container)
	coverUnknown              // the scope cannot be classified
)

func scopeCovers(scope string, target sdk.ARMResource) cover {
	switch armexpr.ClassifyScope(scope) {
	case armexpr.ScopeBroad:
		return coverFull
	case armexpr.ScopeUnknown:
		return coverUnknown
	}
	ref := armexpr.ParseRef(scope)
	self := armexpr.ParseName(target.Type, target.Name)
	if ref.Opaque || len(ref.Segments) == 0 || len(self.Segments) == 0 {
		return coverUnknown
	}
	if !strings.HasPrefix(ref.Type+"/", self.Type+"/") {
		return coverOther
	}
	if len(ref.Segments) < len(self.Segments) {
		return coverOther
	}
	prefix := armexpr.Ref{Type: self.Type, Segments: ref.Segments[:len(self.Segments)]}
	switch armexpr.Compare(prefix, self) {
	case armexpr.Same:
		if len(ref.Segments) == len(self.Segments) {
			return coverFull
		}
		return coverNarrow
	case armexpr.Different:
		return coverOther
	}
	return coverUnknown
}

type roleResult int

const (
	roleMissing roleResult = iota
	roleSatisfied
	roleUncertain
)

// roleStatus looks for an assignment of role to owner that covers target.
func roleStatus(in *sdk.Input, assignments []sdk.ARMResource, owner, target sdk.ARMResource, role roleReq) (roleResult, string) {
	result, why := roleMissing, ""
	uncertain := func(msg string) {
		if result != roleUncertain {
			result, why = roleUncertain, msg
		}
	}
	for _, as := range assignments {
		def, _ := rawStr(as.Properties, "roleDefinitionId")
		guid, gwhy, ok := resolveRoleDefinition(in, def)
		pid, _ := rawStr(as.Properties, "principalId")
		pm := matchPrincipal(pid, owner)
		if pm == triNo {
			continue
		}
		cv := scopeCovers(as.Scope, target)
		if cv == coverOther {
			continue
		}
		if ok && guid != role.guid {
			// A different role of the same principal at a covering scope. A role from the
			// catalogue's own table is judged by its own check. Any other role (a custom
			// role, or a built-in role outside the table) may include the required actions,
			// and the template cannot show its definition, so the result is uncertain and
			// never "missing".
			if !isCatalogueRole(guid) {
				uncertain("the project identity holds another role (" + guid + ") at a covering scope, which may be a custom role or a superset that includes " + role.name + "; the template cannot show its actions")
			}
			continue
		}
		switch {
		case !ok:
			uncertain("a role assignment for the project identity has a role definition that cannot be resolved: " + gwhy)
		case presence(as) != armexpr.Deployed:
			uncertain("the matching role assignment is conditional")
		case pm == triUnknown:
			uncertain("the principal of a role assignment is an unresolved expression that may be the project identity")
		case cv == coverUnknown:
			uncertain("the scope of a matching role assignment cannot be classified")
		case cv == coverNarrow:
			uncertain("the role is assigned on a child scope of the store (for example a container), which the template cannot tie to the container the project uses")
		default:
			return roleSatisfied, ""
		}
	}
	return result, why
}

type owner struct {
	res      sdk.ARMResource
	identity string
}

func identityKind(r sdk.ARMResource) (string, bool, bool) {
	if len(r.Identity) == 0 {
		return "", false, false
	}
	t, present, isExpr := strOf(r.Identity, "type")
	switch {
	case isExpr:
		return "", true, true
	case !present || strings.TrimSpace(t) == "" || strings.EqualFold(strings.TrimSpace(t), "None"):
		return "", false, false
	}
	lt := strings.ToLower(t)
	sys, usr := strings.Contains(lt, "systemassigned"), strings.Contains(lt, "userassigned")
	switch {
	case sys && usr:
		return "system- and user-assigned", true, false
	case usr:
		return "user-assigned", true, false
	}
	return "system-assigned", true, false
}

func evalIDN002(in *sdk.Input) sdk.Result {
	var a acc
	var infos []string
	accounts := ofType(in, typeAccounts)
	var owners []owner
	for _, p := range ofType(in, typeProjects) {
		if a.maybe(p) {
			continue
		}
		kind, has, isExpr := identityKind(p)
		if isExpr {
			a.unres(p, "identity.type is an unresolved expression")
			continue
		}
		if has {
			owners = append(owners, owner{p, kind})
		}
	}
	for _, acct := range accounts {
		if foundrylink.Owns(in.ARM, acct, len(accounts), typeProjects) != foundrylink.OwnsNone {
			continue // the project identity is the actor for accounts that own projects
		}
		if !strings.EqualFold(acct.Kind, "AIServices") || a.maybe(acct) {
			continue
		}
		kind, has, isExpr := identityKind(acct)
		if isExpr {
			a.unres(acct, "identity.type is an unresolved expression")
			continue
		}
		if has {
			owners = append(owners, owner{acct, kind})
		}
	}
	if len(owners) == 0 {
		if a.unresolved {
			return skip(SkipUnresolved + ": the managed identity of a Foundry project or account is an unresolved expression")
		}
		return skip(sdk.SkipNotApplicable + ": no Foundry project or account with a managed identity in the compiled template")
	}
	var targets []sdk.ARMResource
	for _, t := range []string{typeSearch, typeStorage, typeCosmos, typeKeyVault} {
		targets = append(targets, ofType(in, t)...)
	}
	global := foundrylink.Collect(in.ARM)
	hasCapSettings := false
	for _, r := range ofType(in, typeAccounts, typeProjects) {
		if _, ok := r.Properties["capabilitySettings"]; ok {
			hasCapSettings = true
		}
	}
	assignments := ofType(in, typeRoleAssign)
	linkedAny := false
	type pair struct {
		o owner
		t sdk.ARMResource
	}
	var linked []pair
	for _, o := range owners {
		uncertainTargets := 0
		same, maybe := ownerSets(in, o.res)
		for _, t := range targets {
			st := classifyTarget(same, maybe, global, hasCapSettings, len(owners), t)
			switch st {
			case foundrylink.Linked:
				if presence(t) != armexpr.Deployed {
					a.conditional = true
					a.notes = append(a.notes, foundrylink.SkipConditional+": "+shortName(t.Name)+": linked store is deployed only under a condition or loop")
					continue
				}
				linked = append(linked, pair{o, t})
				linkedAny = true
			case foundrylink.Uncertain:
				uncertainTargets++
			}
		}
		if uncertainTargets > 0 {
			a.scopeUncertain = true
			a.notes = append(a.notes, fmt.Sprintf("%s: %s: %d store(s) may be linked to this identity but the template cannot show it", foundrylink.SkipScopeUncertain, shortName(o.res.Name), uncertainTargets))
		}
	}
	if !linkedAny && len(a.fs) == 0 {
		switch {
		case a.scopeUncertain:
			return skip(foundrylink.SkipScopeUncertain)
		case a.conditional:
			return skip(foundrylink.SkipConditional)
		case a.unresolved:
			return skip(SkipUnresolved)
		}
		return skip(sdk.SkipNotApplicable + ": no Storage, AI Search, Cosmos DB or Key Vault resource is linked to a project identity")
	}
	if len(assignments) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{
			Reason:            sdk.SkipInputUnavailable + ": no Microsoft.Authorization/roleAssignments resource in the compiled template, so role assignments cannot be checked statically",
			MissingCapability: idn002Capability,
		}}
	}
	for _, p := range linked {
		o, t := p.o, p.t
		for _, role := range requiredRoles(t.Type) {
			res, why := roleStatus(in, assignments, o.res, t, role)
			switch res {
			case roleMissing:
				a.addKey(o.res, "role:"+role.guid+":"+shortName(t.Name), fmt.Sprintf(
					"%s (%s identity) has no %s (%s) role assignment on %s %s in the compiled template",
					kindName(o.res), o.identity, role.name, role.guid, resourceKind(t.Type), shortName(t.Name)))
			case roleUncertain:
				a.unres(o.res, role.name+" on "+shortName(t.Name)+": "+why)
			}
		}
		switch strings.ToLower(t.Type) {
		case strings.ToLower(typeStorage):
			if res, _ := roleStatus(in, assignments, o.res, t, roleStorageAccountContributor); res == roleMissing {
				infos = append(infos, fmt.Sprintf("%s: %s: no %s assignment on storage account %s; the documentation lists it for the system-assigned identity but it is not confirmed as required, so it is not a finding", sdk.SkipNotApplicable, shortName(o.res.Name), roleStorageAccountContributor.name, shortName(t.Name)))
			}
		case strings.ToLower(typeCosmos):
			a.unres(o.res, "Cosmos DB Built-in Data Contributor SQL role on "+shortName(t.Name)+": the SQL role definition GUID is not verified, so SQL data-plane access cannot be proven from the template")
		case strings.ToLower(typeKeyVault):
			infos = append(infos, fmt.Sprintf("%s: %s: Key Vault %s is linked but the catalogue lists no verified required data role for it, so no role is checked", sdk.SkipNotApplicable, shortName(o.res.Name), shortName(t.Name)))
		}
	}
	notes := append(append([]string(nil), a.notes...), infos...)
	switch {
	case len(a.fs) > 0:
		return withSkips(a.fs, notes)
	case a.scopeUncertain:
		return skip(foundrylink.SkipScopeUncertain)
	case a.conditional:
		return skip(foundrylink.SkipConditional)
	case a.unresolved:
		return skip(SkipUnresolved + ": " + joinReasons(a.notes))
	}
	return withSkips(nil, infos)
}

func kindName(r sdk.ARMResource) string {
	if strings.EqualFold(r.Type, typeProjects) {
		return "project " + shortName(r.Name)
	}
	return "account " + shortName(r.Name)
}

func resourceKind(t string) string {
	switch strings.ToLower(t) {
	case strings.ToLower(typeSearch):
		return "AI Search service"
	case strings.ToLower(typeStorage):
		return "storage account"
	case strings.ToLower(typeCosmos):
		return "Cosmos DB account"
	}
	return "key vault"
}

func joinReasons(notes []string) string {
	sorted := append([]string(nil), notes...)
	sort.Strings(sorted)
	var out []string
	for i, n := range sorted {
		if i > 0 && sorted[i-1] == n {
			continue
		}
		out = append(out, n)
	}
	if len(out) > 3 {
		return strings.Join(out[:3], "; ") + fmt.Sprintf("; and %d more", len(out)-3)
	}
	return strings.Join(out, "; ")
}

// ownerSets builds the link sets for one identity owner: references held by
// connections and capability hosts that provably belong to it (same) and by
// ones that may (maybe).
func ownerSets(in *sdk.Input, o sdk.ARMResource) (same, maybe foundrylink.Set) {
	self := armexpr.ParseName(o.Type, o.Name)
	scoped := func(want armexpr.Result) func(sdk.ARMResource) bool {
		return func(c sdk.ARMResource) bool {
			ref := armexpr.ParseName(c.Type, c.Name)
			n := len(self.Segments)
			if ref.Opaque || len(ref.Segments) < 2 {
				return want == armexpr.Unknown
			}
			// A connection or capability host named acct/x belongs to the account
			// (shared by its projects); acct/proj/x belongs to the project.
			var owned armexpr.Ref
			switch len(ref.Segments) {
			case 2:
				owned = armexpr.Ref{Type: strings.ToLower(typeAccounts), Segments: ref.Segments[:1]}
				return armexpr.Compare(owned, armexpr.Ref{Type: strings.ToLower(typeAccounts), Segments: self.Segments[:1]}) == want
			default:
				if n < 2 {
					// The identity owner is an account: project-level resources do not count.
					return false
				}
				owned = armexpr.Ref{Type: strings.ToLower(typeProjects), Segments: ref.Segments[:2]}
				return armexpr.Compare(owned, self) == want
			}
		}
	}
	return foundrylink.CollectWhere(in.ARM, scoped(armexpr.Same)), foundrylink.CollectWhere(in.ARM, scoped(armexpr.Unknown))
}

func classifyTarget(same, maybe, global foundrylink.Set, hasCapSettings bool, owners int, t sdk.ARMResource) foundrylink.State {
	switch same.Classify(t) {
	case foundrylink.Linked:
		return foundrylink.Linked
	case foundrylink.Uncertain:
		return foundrylink.Uncertain
	}
	if maybe.Classify(t) == foundrylink.Linked {
		return foundrylink.Uncertain
	}
	if hasCapSettings && global.Classify(t) == foundrylink.Linked {
		// Linked through capabilitySettings (not attributable to one project) or
		// through another project's connection.
		if owners == 1 {
			return foundrylink.Linked
		}
		return foundrylink.Uncertain
	}
	return foundrylink.NotLinked
}
