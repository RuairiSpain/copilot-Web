package rules_test

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	rulesdata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

// TestPackEqualsPhase1Catalogue: the foundry-core pack lists exactly the catalogue's phase 1 rules.
func TestPackEqualsPhase1Catalogue(t *testing.T) {
	ctx := context.Background()
	cat, err := rules.LoadCatalog(ctx, rulesdata.FS, rulesdata.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	packs, err := rules.LoadPacks(ctx, rulesdata.FS, rulesdata.PacksRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := rules.ValidatePacks(packs, cat); err != nil {
		t.Fatal(err)
	}
	p, ok := rules.PackByID(packs, rules.DefaultPack)
	if !ok {
		t.Fatal("foundry-core missing")
	}
	var want []string
	for _, m := range cat {
		if rules.InPhase(m, rules.Phase1) && m.Status != "dropped" && m.Status != "deprecated" {
			want = append(want, m.ID)
		}
	}
	if len(want) != 38 {
		t.Errorf("phase 1 catalogue rules = %d, want 38", len(want))
	}
	if !slices.Equal(p.Rules, want) {
		t.Errorf("pack rules differ from the catalogue phase 1 set\npack: %v\nwant: %v", p.Rules, want)
	}
	if _, ok := rules.PackByID(packs, "nope"); ok {
		t.Error("PackByID found a missing pack")
	}
}

// TestPackSchemaMatchesStruct keeps schemas/pack.schema.json in step with the Pack struct.
func TestPackSchemaMatchesStruct(t *testing.T) {
	b, err := os.ReadFile("../../schemas/pack.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Required   []string               `json:"required"`
		Properties map[string]interface{} `json:"properties"`
		Additional bool                   `json:"additionalProperties"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	want := []string{"description", "id", "rules", "schema", "version"}
	var got []string
	for k := range s.Properties {
		got = append(got, k)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) || s.Additional {
		t.Errorf("schema properties %v additional=%v, want %v closed", got, s.Additional, want)
	}
	if !slices.Equal(s.Required, []string{"schema", "id", "version", "rules"}) {
		t.Errorf("required = %v", s.Required)
	}
}

func TestValidatePacks(t *testing.T) {
	cat := testCatalog()
	ok := rules.Pack{Schema: "pack-v1", ID: "p", Version: 1, Rules: []string{"FND-NET-001", "FND-SEC-001"}}
	with := func(mut func(*rules.Pack)) []rules.Pack {
		p := ok
		p.Rules = slices.Clone(ok.Rules)
		mut(&p)
		return []rules.Pack{p}
	}
	tests := []struct {
		name  string
		packs []rules.Pack
		want  string
	}{
		{"valid", []rules.Pack{ok}, ""},
		{"schema", with(func(p *rules.Pack) { p.Schema = "v2" }), "schema must be"},
		{"id", with(func(p *rules.Pack) { p.ID = "Bad_ID" }), "id must match"},
		{"version", with(func(p *rules.Pack) { p.Version = 0 }), "version must be"},
		{"empty", with(func(p *rules.Pack) { p.Rules = nil }), "rules is empty"},
		{"unsorted", with(func(p *rules.Pack) { p.Rules = []string{"FND-SEC-001", "FND-NET-001"} }), "must be sorted"},
		{"duplicate rule", with(func(p *rules.Pack) { p.Rules = []string{"FND-SEC-001", "FND-SEC-001"} }), "duplicates"},
		{"unknown rule", with(func(p *rules.Pack) { p.Rules = []string{"FND-ZZZ-001"} }), "not in the catalogue"},
		{"duplicate pack", []rules.Pack{ok, ok}, "duplicate pack id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := rules.ValidatePacks(tc.packs, cat)
			switch {
			case tc.want == "" && err != nil:
				t.Fatal(err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("error %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadPacksErrors(t *testing.T) {
	good := "schema: pack-v1\nid: a\nversion: 1\nrules: [FND-SEC-001]\n"
	tests := []struct {
		name string
		fsys fstest.MapFS
		want string
	}{
		{"unknown field", fstest.MapFS{"p/a.yaml": {Data: []byte(good + "extra: 1\n")}}, "extra"},
		{"id differs from file", fstest.MapFS{"p/b.yaml": {Data: []byte(good)}}, "file name must equal"},
		{"two documents", fstest.MapFS{"p/a.yaml": {Data: []byte(good + "---\n" + good)}}, "more than one"},
		{"not yaml file", fstest.MapFS{"p/a.txt": {Data: []byte(good)}}, "unexpected entry"},
		{"missing dir", fstest.MapFS{}, "read packs"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := rules.LoadPacks(context.Background(), tc.fsys, "p")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v, want %q", err, tc.want)
			}
		})
	}
	packs, err := rules.LoadPacks(context.Background(), fstest.MapFS{"p/a.yaml": {Data: []byte(good)}}, "p")
	if err != nil || len(packs) != 1 || packs[0].ID != "a" {
		t.Fatalf("good pack: %v %v", packs, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rules.LoadPacks(ctx, fstest.MapFS{"p/a.yaml": {Data: []byte(good)}}, "p"); err == nil {
		t.Fatal("cancelled context ignored")
	}
}
