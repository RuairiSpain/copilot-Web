package idn

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// idn002 implements the offline half of FND-IDN-002: the project identity has the required data roles.
// The live half (Phase 2) is reported as skipped with control-plane-unavailable.
type idn002 struct{ meta }

func newIDN002() rules.Rule { return idn002{meta{"FND-IDN-002", 1}} }

type storeKind int

const (
	storeSearch storeKind = iota
	storeStorage
	storeCosmos
	storeRegistry
)

// cosmosDataContributorID is the Cosmos DB Built-in Data Contributor SQL role definition ID used by the Foundry
// samples. It is corroborated by usage only, not primary-verified, so a match is reported as likely.
const cosmosDataContributorID = "00000000-0000-0000-0000-000000000002"

// roleReq is one required role for a store kind. GUIDs are from the catalogue (Azure built-in roles reference).
type roleReq struct {
	key, name string
	guids     []string
	container string // for Storage: lower-case fragment of the container name the role is documented on
	sql       bool   // Cosmos DB SQL role assignment instead of an Azure RBAC assignment
}

var storeReqs = map[storeKind][]roleReq{
	storeSearch: {
		{key: "search-index-data-contributor", name: "Search Index Data Contributor", guids: []string{"8ebe5a00-799e-43f5-93ac-243d3dce84a7"}},
		{key: "search-service-contributor", name: "Search Service Contributor", guids: []string{"7ca78c08-252a-4471-8644-bb5ff32d4ba0"}},
	},
	storeStorage: {
		{key: "storage-blob-data-contributor", name: "Storage Blob Data Contributor", guids: []string{"ba92f5b4-2d11-453d-a403-e96b0029c9fe"}, container: "blobstore"},
		{key: "storage-blob-data-owner", name: "Storage Blob Data Owner", guids: []string{"b7e6dc6d-f1e8-4753-8033-0f276bb0955b"}, container: "agent"},
	},
	storeCosmos: {
		{key: "cosmos-db-operator", name: "Cosmos DB Operator", guids: []string{"230815da-be43-4aae-9cb4-875f7bd000aa"}},
		{key: "cosmos-sql-data-contributor", name: "Cosmos DB Built-in Data Contributor (SQL role)", guids: []string{cosmosDataContributorID}, sql: true},
	},
	storeRegistry: {
		{key: "registry-pull", name: "AcrPull or Container Registry Repository Reader",
			guids: []string{"7f951dda-4ed3-4680-a7ca-43fe172d538d", "b93aa761-3e63-49ed-ac28-beffa264f7ac"}},
	},
}

// store is a resource the project uses.
type store struct {
	kind storeKind
	ref  idRef
	name string
}

func (s store) label() string {
	switch s.kind {
	case storeSearch:
		return "Azure AI Search " + s.name
	case storeStorage:
		return "Storage account " + s.name
	case storeCosmos:
		return "Cosmos DB account " + s.name
	}
	return "Container Registry " + s.name
}

func kindOfRef(ref idRef) (storeKind, bool) {
	if ref.kind != idResource {
		return 0, false
	}
	switch strings.ToLower(ref.rootType()) {
	case "microsoft.search/searchservices":
		return storeSearch, true
	case "microsoft.storage/storageaccounts":
		return storeStorage, true
	case "microsoft.documentdb/databaseaccounts":
		return storeCosmos, true
	case "microsoft.containerregistry/registries":
		return storeRegistry, true
	}
	return 0, false
}

// storesOf collects the stores a project uses from the capability settings (preview API) of the account and the
// project and from the project connections (properties.metadata.ResourceId). It also returns the details of
// store IDs it could not read.
func storesOf(t *model.ARMTemplate, project model.Resource) (stores []store, unreadable []string) {
	seen := map[string]bool{}
	add := func(raw, where string) {
		ref := parseID(raw)
		k, ok := kindOfRef(ref)
		if !ok {
			if ref.kind == idUnknown || ref.kind == idSymbolic {
				unreadable = append(unreadable, where)
			}
			return
		}
		name := ref.rootType()
		if len(ref.names) > 0 {
			name = ref.names[0]
		}
		id := fmt.Sprintf("%d|%s", k, nameKey(name))
		if seen[id] {
			return
		}
		seen[id] = true
		stores = append(stores, store{kind: k, ref: ref, name: name})
	}
	pSegs := nameSegments(project.Name)
	pLast := nameKey(pSegs[len(pSegs)-1])
	owners := []model.Resource{project}
	for _, a := range t.ResourcesOfType(typeAccount) {
		aSegs := nameSegments(a.Name)
		if len(pSegs) < 2 || nameKey(pSegs[0]) == nameKey(aSegs[len(aSegs)-1]) || len(t.ResourcesOfType(typeAccount)) == 1 {
			owners = append(owners, a)
		}
	}
	for _, o := range owners {
		for _, key := range []string{"documentStore", "vectorStore", "blobStore"} {
			if s, ok := rawString(o, "properties", "capabilitySettings", key); ok && s != "" {
				add(s, "capabilitySettings."+key)
			}
		}
	}
	projects := len(t.ResourcesOfType(typeProject))
	for _, c := range t.ResourcesOfType(typeConnection) {
		cSegs := nameSegments(c.Name)
		if projects > 1 && !(len(cSegs) >= 2 && nameKey(cSegs[len(cSegs)-2]) == pLast) {
			continue
		}
		if s, ok := rawString(c, "properties", "metadata", "ResourceId"); ok && s != "" {
			add(s, "connection "+c.Name)
		}
	}
	return stores, unreadable
}

type belongs int

const (
	belongsNo belongs = iota
	belongsYes
	belongsUnknown
)

func projectOwns(t *model.ARMTemplate, project model.Resource, id identKind, ra model.Resource) belongs {
	p := resolvePrincipal(t, ra)
	switch p.kind {
	case principalParam, principalModule, principalUnknown:
		return belongsUnknown
	case principalRef:
		if p.field == fieldUnknown || p.ref.kind == idUnknown {
			return belongsUnknown
		}
		if p.field != fieldObjectID {
			return belongsNo
		}
		viaSystem := p.system && id.hasSystem() && containsRes(p.cands, project)
		viaUser := false
		att := attachedIdentities(t, project)
		for _, c := range p.cands {
			if isType(c, typeUAMI) && containsRes(att, c) {
				viaUser = true
			}
		}
		if viaSystem || viaUser {
			if len(p.cands) > 1 && !viaUser {
				return belongsUnknown
			}
			return belongsYes
		}
	}
	return belongsNo
}

type coverage int

const (
	coverNone coverage = iota
	coverFull
	coverContainerUnsure
	coverUnknown
)

func sameResource(t *model.ARMTemplate, a, b idRef) bool {
	if len(a.names) > 0 && len(b.names) > 0 && nameKey(a.names[0]) == nameKey(b.names[0]) {
		return true
	}
	root := func(x idRef) []model.Resource {
		if len(x.names) == 0 {
			return nil
		}
		return match(t, idRef{kind: idResource, typ: x.rootType(), names: x.names[:1]})
	}
	ma, mb := root(a), root(b)
	return len(ma) == 1 && len(mb) == 1 && samePointer(ma[0], mb[0])
}

func covers(t *model.ARMTemplate, ra model.Resource, s store, container string) coverage {
	scope, present := scopeOf(ra)
	if !present {
		return coverFull
	}
	switch {
	case scope.broad():
		return coverFull
	case scope.kind != idResource:
		return coverUnknown
	}
	if !strings.EqualFold(scope.rootType(), s.ref.rootType()) || !sameResource(t, scope, s.ref) {
		return coverNone
	}
	if !scope.isChild() {
		return coverFull
	}
	if container == "" || !strings.HasSuffix(strings.ToLower(scope.typ), "/containers") {
		return coverNone
	}
	n := scope.names[len(scope.names)-1]
	if strings.ContainsAny(n, "(){") {
		return coverContainerUnsure
	}
	if strings.Contains(strings.ToLower(n), container) {
		return coverFull
	}
	return coverNone
}

// cosmosAssociated reports whether a SQL role assignment is lexically tied to the Cosmos DB store.
func cosmosAssociated(t *model.ARMTemplate, ra model.Resource, s store) bool {
	if len(t.ResourcesOfType("Microsoft.DocumentDB/databaseAccounts")) == 1 {
		return true
	}
	hay := strings.ToLower(strings.ReplaceAll(ra.Name, " ", ""))
	for _, p := range []string{"scope", "roleDefinitionId"} {
		v, _ := rawString(ra, "properties", p)
		hay += "|" + strings.ToLower(strings.ReplaceAll(v, " ", ""))
	}
	k := nameKey(s.name)
	return k != "" && strings.Contains(hay, k)
}

func (r idn002) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	if res, stop := armGuard(r, in); stop {
		return res, nil
	}
	t := in.ARM
	var out []rules.Result
	for _, p := range t.ResourcesOfType(typeProject) {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("FND-IDN-002: %w", err)
		}
		out = append(out, r.project(t, p)...)
	}
	out = append(out, liveSkip(r, "live role assignments are read in Phase 2; this run checked the template only"))
	return out, nil
}

func (r idn002) project(t *model.ARMTemplate, p model.Resource) []rules.Result {
	id := identityOf(p)
	switch id {
	case identAbsent, identNone:
		return []rules.Result{skipRes(r, p, rules.ReasonNotApplicable, "the project declares no managed identity (see FND-IDN-001), so there is no principal to check", "project managed identity")}
	case identParam, identModule, identUnresolved:
		return []rules.Result{skipRes(r, p, rules.ReasonUnresolvedValue("principal"), "the project identity type is not a literal, so its principal cannot be resolved", "resolved project identity")}
	}
	stores, unreadable := storesOf(t, p)
	var out []rules.Result
	for _, w := range unreadable {
		out = append(out, skipRes(r, p, rules.ReasonUnresolvedValue("store-id"), "the store ID in "+w+" cannot be read statically", "resolved resource ID"))
	}
	if len(stores) == 0 && len(unreadable) == 0 {
		return []rules.Result{rules.Pass()}
	}
	for _, s := range stores {
		for _, req := range storeReqs[s.kind] {
			out = append(out, r.require(t, p, id, s, req))
		}
	}
	return out
}

func (r idn002) require(t *model.ARMTemplate, p model.Resource, id identKind, s store, req roleReq) rules.Result {
	typ := typeRoleAssign
	if req.sql {
		typ = typeCosmosRole
	}
	var unresolved, unsure bool
	for _, ra := range t.ResourcesOfType(typ) {
		raw, _ := rawString(ra, "properties", "roleDefinitionId")
		guid, roleOK, _ := roleGUIDText(raw)
		own := projectOwns(t, p, id, ra)
		if own == belongsNo {
			continue
		}
		cv := coverFull
		if req.sql {
			if !cosmosAssociated(t, ra, s) {
				cv = coverNone
			}
		} else {
			cv = covers(t, ra, s, req.container)
		}
		if cv == coverNone {
			continue
		}
		if !roleOK {
			unresolved = true
			continue
		}
		matched := false
		for _, g := range req.guids {
			matched = matched || g == guid
		}
		switch {
		case !matched:
		case own == belongsUnknown || cv == coverUnknown:
			unresolved = true
		case cv == coverContainerUnsure:
			unsure = true
		default:
			return rules.Pass()
		}
	}
	key := req.key + "@" + s.name
	switch {
	case unresolved:
		return skipRes(r, p, rules.ReasonUnresolvedValue("principal"),
			fmt.Sprintf("an assignment of %s on %s has a principal, scope or role that cannot be resolved statically", req.name, s.label()), "resolved role assignment")
	case unsure:
		return uncertain(r, p, key, "",
			fmt.Sprintf("project %s: %s on %s is assigned at a container scope whose name is an expression (it contains the project workspace ID); the container cannot be confirmed statically", p.Name, req.name, s.label()), rec002, fix002)
	}
	conf := sdk.ConfidenceLikely
	note := ""
	if req.sql {
		note = "; the SQL role ID " + cosmosDataContributorID + " is corroborated by Microsoft samples, not primary-verified"
	}
	return fail(r, p, key, "", fmt.Sprintf("project %s: no role assignment for %s on %s in the template%s", p.Name, req.name, s.label(), note), rec002, fix002, conf)
}
