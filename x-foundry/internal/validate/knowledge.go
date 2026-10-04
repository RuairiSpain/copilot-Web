package validate

import (
	"fmt"
	"math"
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
)

type embeddingLimit struct {
	max      int
	flexible bool // supports requesting fewer dimensions
}

var embeddingModels = map[string]embeddingLimit{
	"text-embedding-3-large": {3072, true},
	"text-embedding-3-small": {1536, true},
	"text-embedding-ada-002": {1536, false},
}

func scopePath(scope string) string {
	switch scope {
	case "root":
		return root
	case "hub":
		return root + ".hub"
	}
	return fmt.Sprintf("%s.projects[%s]", root, strings.TrimPrefix(scope, "project:"))
}

func itemPath(scope, collection, name, tail string) string {
	if collection == "knowledgeBases" {
		collection = "iq.knowledgeBases"
	}
	p := fmt.Sprintf("%s.%s[%s]", scopePath(scope), collection, name)
	if tail != "" {
		p += "." + tail
	}
	return p
}

// validateKnowledgeBase checks one knowledge base: rules 7-12 and 117/118.
func validateKnowledgeBase(kb *config.KnowledgeBase, scope string, deployments []config.ModelDeployment) []diag.Diagnostic {
	var out []diag.Diagnostic
	at := func(tail string) string { return itemPath(scope, "knowledgeBases", kb.Name, tail) }
	index, vector, retrieval := &kb.Index, &kb.Index.Vector, &kb.Retrieval
	fields := map[string]config.IndexField{}
	for _, f := range index.Fields {
		fields[f.Name] = f
	}

	if index.Chunking.Overlap >= index.Chunking.Size {
		out = append(out, diag.Err("XF011", at("index.chunking.overlap"),
			"chunking.overlap (%d) must be less than chunking.size (%d)", index.Chunking.Overlap, index.Chunking.Size))
	}
	if retrieval.Mode == "hybrid" && math.Abs(retrieval.VectorWeight+retrieval.KeywordWeight-1.0) > 1e-9 {
		out = append(out, diag.Err("XF012", at("retrieval"),
			"hybrid retrieval weights must sum to 1.0 (vectorWeight %g + keywordWeight %g)", retrieval.VectorWeight, retrieval.KeywordWeight))
	}

	need := func(role, name string, ok bool, expectation string) {
		if _, found := fields[name]; !found {
			out = append(out, diag.Err("XF009", at("index."+role), "%s '%s' is not defined in index.fields", role, name))
		} else if !ok {
			out = append(out, diag.Err("XF009", at("index."+role), "%s '%s' must %s", role, name, expectation))
		}
	}
	key, content, title := fields[index.KeyField], fields[index.ContentField], fields[index.TitleField]
	need("keyField", index.KeyField, key.Key && key.Type == "Edm.String", "be an Edm.String with key: true")
	need("contentField", index.ContentField, content.Type == "Edm.String" && content.Searchable, "be a searchable Edm.String")
	need("titleField", index.TitleField, title.Type == "Edm.String", "be an Edm.String")
	var keys []string
	for _, f := range index.Fields {
		if f.Key {
			keys = append(keys, f.Name)
		}
	}
	if len(keys) > 1 {
		out = append(out, diag.Err("XF009", at("index.fields"), "index declares several key fields (%s); exactly one is allowed", strings.Join(keys, ", ")))
	}
	if index.Semantic.Enabled {
		for _, name := range append([]string{index.Semantic.TitleField}, append(index.Semantic.ContentFields, index.Semantic.KeywordFields...)...) {
			if _, ok := fields[name]; !ok {
				out = append(out, diag.Err("XF009", at("index.semantic"), "semantic configuration references undefined field '%s'", name))
			}
		}
	}

	for _, f := range index.Fields {
		if f.Type == "Collection(Edm.Single)" && (f.Dimensions == 0 || f.VectorProfile == "") {
			out = append(out, diag.Err("XF010", at("index.fields["+f.Name+"]"), "vector field '%s' must declare dimensions and vectorProfile", f.Name))
		}
	}
	if vector.Enabled {
		vf, ok := fields[index.VectorField]
		switch {
		case !ok:
			out = append(out, diag.Err("XF009", at("index.vectorField"), "vectorField '%s' is not defined in index.fields", index.VectorField))
		case vf.Type != "Collection(Edm.Single)":
			out = append(out, diag.Err("XF010", at("index.vectorField"), "vectorField '%s' must be Collection(Edm.Single), not %s", vf.Name, vf.Type))
		default:
			if vf.Dimensions != vector.Dimensions {
				out = append(out, diag.Err("XF007", at("index.fields["+vf.Name+"].dimensions"),
					"vector field '%s' has %d dimensions but index.vector.dimensions is %d", vf.Name, vf.Dimensions, vector.Dimensions))
			}
			if vf.VectorProfile != vector.Profile {
				out = append(out, diag.Err("XF010", at("index.fields["+vf.Name+"].vectorProfile"),
					"vector field '%s' uses profile '%s' but index.vector.profile is '%s'", vf.Name, vf.VectorProfile, vector.Profile))
			}
		}
		out = append(out, embedding(kb, scope, deployments)...)
	} else if retrieval.Mode == "vector" || retrieval.Mode == "hybrid" {
		out = append(out, diag.Err("XF117", at("retrieval.mode"), "retrieval mode '%s' requires index.vector.enabled", retrieval.Mode))
	}
	if kb.Routing.Fallback == "vector" && !vector.Enabled {
		out = append(out, diag.Err("XF117", at("routing.fallback"), "routing.fallback 'vector' requires index.vector.enabled"))
	}

	for _, name := range retrieval.FilterFields {
		f, ok := fields[name]
		switch {
		case !ok:
			out = append(out, diag.Err("XF008", at("retrieval.filterFields"), "filterFields references undefined field '%s'", name))
		case !f.Filterable:
			out = append(out, diag.Err("XF008", at("retrieval.filterFields"), "filterFields references '%s' which is not filterable: true", name))
		}
	}
	claims := make([]string, 0, len(kb.Access.FilterClaims))
	for c := range kb.Access.FilterClaims {
		claims = append(claims, c)
	}
	sortStrings(claims)
	for _, claim := range claims {
		name := kb.Access.FilterClaims[claim]
		if f, ok := fields[name]; !ok || !f.Filterable {
			out = append(out, diag.Err("XF008", at("access.filterClaims"), "filterClaims maps claim '%s' to '%s', which must be a filterable index field", claim, name))
		}
	}

	if retrieval.SemanticRanking && !index.Semantic.Enabled {
		out = append(out, diag.Err("XF117", at("retrieval.semanticRanking"), "retrieval.semanticRanking requires index.semantic.enabled"))
	}
	if kb.Routing.Strategy == "explicit" && len(kb.Routing.Routes) == 0 {
		out = append(out, diag.Err("XF118", at("routing.routes"), "routing strategy 'explicit' needs at least one route"))
	}
	return out
}

func embedding(kb *config.KnowledgeBase, scope string, deployments []config.ModelDeployment) []diag.Diagnostic {
	vector := kb.Index.Vector
	where := itemPath(scope, "knowledgeBases", kb.Name, "index.vector")
	var d *config.ModelDeployment
	for i := range deployments {
		if deployments[i].Name == vector.Deployment {
			d = &deployments[i]
		}
	}
	if d == nil {
		return []diag.Diagnostic{diag.Err("XF005", where+".deployment", "embedding deployment '%s' does not exist", vector.Deployment)}
	}
	if d.Model != vector.Model {
		return []diag.Diagnostic{diag.Err("XF007", where+".model", "deployment '%s' serves '%s' but index.vector.model is '%s'", d.Name, d.Model, vector.Model)}
	}
	limit, known := embeddingModels[d.Model]
	switch {
	case !known:
		return []diag.Diagnostic{diag.Warn("XF007", where+".dimensions", "'%s' is not a known embedding model; dimensions cannot be verified", d.Model)}
	case limit.flexible && vector.Dimensions > limit.max:
		return []diag.Diagnostic{diag.Err("XF007", where+".dimensions", "%s produces at most %d dimensions, not %d", d.Model, limit.max, vector.Dimensions)}
	case !limit.flexible && vector.Dimensions != limit.max:
		return []diag.Diagnostic{diag.Err("XF007", where+".dimensions", "%s produces exactly %d dimensions, not %d", d.Model, limit.max, vector.Dimensions)}
	}
	return nil
}
