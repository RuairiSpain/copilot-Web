package azureyaml

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/schemas"
)

// recordedSHA256 is the hash of every vendored schema file. Changing a vendored file, or refreshing the vendoring,
// must update this table, schemas/vendor/azd/SHA256SUMS and schemas/vendor/azd/README.md together.
// Source: azd tag azure-dev-cli_1.35.0 (170ebb8) for azure.yaml.json; commit a64ca8f for the azure.ai.* files;
// only $id and absolute $ref URLs were rewritten (scripts/ci/vendor-azd-schemas.sh).
var recordedSHA256 = map[string]string{
	"azure.ai.agents/Agent.json":                    "f8ad1e8d3503e29bf3a63ca1284a3cc3e4e87e577751e814581978cfd8c6da4a",
	"azure.ai.agents/Connection.json":               "088aeb5a68ee72b6595431bfdbb9314f69b31dc143d4c976f255fd5816049a13",
	"azure.ai.agents/Deployment.json":               "bf526420cfcddc5fb28034562eafd96302369182f358f9cb4ce44298a2575927",
	"azure.ai.agents/FileRef.json":                  "accca4272c802da61e3199f67c93fe767c5743c95ad734ff2240cc40e1d437c4",
	"azure.ai.agents/Routine.json":                  "16bc5887eef90b845c4a09b0fe60a44d9e2a2892673ff2e0ca5570f77d5a5f98",
	"azure.ai.agents/Skill.json":                    "c0033be7e629cc4598dcb85b762415e55c4ef2eaa86e5a053367da335c02cc48",
	"azure.ai.agents/Toolbox.json":                  "69ae94c2dfad2faf5f1db78da25f6e23e60c91a0a999f54dd246ecc5836e1fe0",
	"azure.ai.agents/azure.ai.agent.json":           "0b133fbbb529c12d0e7b35f1c67535b34d396384564fa5d0bbe65c76f06e105b",
	"azure.ai.agents/microsoft.foundry.json":        "97d8b04d0f4493c1744a70c7ed18560a3ff6a98f83279eedf038a79ebd79c58a",
	"azure.ai.connections/azure.ai.connection.json": "1bebcf28dce5d29163aedf01e7af784f1d3b39846636a922dbc21ff9226ecea1",
	"azure.ai.evaluations/azure.ai.eval.json":       "0d38c05dd94acabbdb1d90f331f9418834dbc036b1ab07c2091478457c50f207",
	"azure.ai.projects/azure.ai.project.json":       "2de31fc9e26e5e5af460a81fb8d39c242f10bbf93313d2745739f9c5c7d4e0e3",
	"azure.ai.routines/azure.ai.routine.json":       "fd451f1ef36eb97f0c5334df110b42a946575a22a279e8496d54c7c5a6a6a6c8",
	"azure.ai.skills/azure.ai.skill.json":           "14fa2ae7457713b2bd2acc752ceecc7e2a2528c4fb638b92c003eaa8f0e796aa",
	"azure.ai.toolboxes/azure.ai.toolbox.json":      "4219f278e472f50210408b4ec75f32f42ec232c9e4ad6b6174b6d3932acac276",
	"azure.yaml.json":                               "62137afd409a58eba63a186bdd1d67858de4e979712237a0ba7fb3a5cd405e34",
}

func embeddedHashes(t *testing.T) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := fs.WalkDir(schemas.AzdFS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, rerr := fs.ReadFile(schemas.AzdFS(), p)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(b)
		got[p] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestVendoredSchemasMatchRecordedHashes(t *testing.T) {
	got := embeddedHashes(t)
	for name, want := range recordedSHA256 {
		switch h, ok := got[name]; {
		case !ok:
			t.Errorf("%s is recorded but not embedded", name)
		case h != want:
			t.Errorf("%s drifted: sha256 %s, recorded %s", name, h, want)
		}
	}
	for name := range got {
		if _, ok := recordedSHA256[name]; !ok {
			t.Errorf("%s is embedded but has no recorded hash", name)
		}
	}
}

func TestSHA256SUMSFileMatchesRecordedHashes(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "schemas", "vendor", "azd", "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	listed := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			t.Fatalf("bad SHA256SUMS line %q", sc.Text())
		}
		listed[fields[1]] = fields[0]
	}
	if len(listed) != len(recordedSHA256) {
		t.Errorf("SHA256SUMS has %d entries, test records %d", len(listed), len(recordedSHA256))
	}
	for name, want := range recordedSHA256 {
		if listed[name] != want {
			t.Errorf("SHA256SUMS %s = %q, test records %q", name, listed[name], want)
		}
	}
}

var remoteURL = regexp.MustCompile(`https?://`)

func TestVendoredRefsAreLocalAndResolve(t *testing.T) {
	s, err := loadSchemas()
	if err != nil {
		t.Fatal(err)
	}
	var walk func(file string, v any)
	walk = func(file string, v any) {
		switch t2 := v.(type) {
		case map[string]any:
			if id, ok := t2["$id"].(string); ok && id != file {
				t.Errorf("%s: $id = %q, want the file's own relative path", file, id)
			}
			if ref, ok := t2["$ref"].(string); ok {
				if remoteURL.MatchString(ref) {
					t.Errorf("%s: remote $ref %q", file, ref)
				}
				if !strings.HasPrefix(ref, "#") {
					target := filepath.ToSlash(filepath.Join(filepath.Dir(file), ref))
					if s.files[target] == nil {
						t.Errorf("%s: $ref %q does not resolve to a vendored file", file, ref)
					}
				}
			}
			for _, c := range t2 {
				walk(file, c)
			}
		case []any:
			for _, c := range t2 {
				walk(file, c)
			}
		}
	}
	for name, doc := range s.files {
		walk(name, doc)
	}
}

func TestRootRefsPointAtVendoredExtensionSchemas(t *testing.T) {
	s, _ := loadSchemas()
	raw, err := fs.ReadFile(schemas.AzdFS(), rootFile)
	if err != nil {
		t.Fatal(err)
	}
	refs := regexp.MustCompile(`"\$ref": "(azure\.ai\.[a-z]+/[A-Za-z.]+\.json)"`).FindAllStringSubmatch(string(raw), -1)
	if len(refs) < 9 {
		t.Fatalf("expected the nine extension refs of the root schema, found %d", len(refs))
	}
	for _, m := range refs {
		if s.files[m[1]] == nil {
			t.Errorf("root $ref %s not vendored", m[1])
		}
	}
}

// The reader derives its checks from the vendored schema. These tests pin the facts it relies on, so a refresh
// that changes them fails loudly instead of silently changing behaviour.
func TestSchemaFactsTheReaderRelaysOn(t *testing.T) {
	s, err := loadSchemas()
	if err != nil {
		t.Fatal(err)
	}
	if got := stringList(s.root["required"]); !slices.Equal(got, []string{"name"}) {
		t.Errorf("root required = %v", got)
	}
	if s.root["additionalProperties"] != true {
		t.Error("root schema no longer allows additional properties; unknown top-level keys would become errors")
	}
	for _, h := range []string{"azure.ai.agent", "azure.ai.project", "azure.ai.connection", "azure.ai.toolbox", "azure.ai.skill", "azure.ai.routine", "azure.ai.eval", "microsoft.foundry", "containerapp"} {
		if !s.knownHosts[h] {
			t.Errorf("host %s not known", h)
		}
	}
	if dig(s.root, "properties", "services", "minProperties") != float64(1) {
		t.Error("services minProperties changed")
	}
	req := stringList(dig(s.root, "properties", "services", "additionalProperties", "required"))
	if !slices.Equal(req, []string{"host"}) {
		t.Errorf("service required = %v", req)
	}
	agent := s.files["azure.ai.agents/azure.ai.agent.json"]
	if got := stringList(dig(agent, "properties", "kind", "enum")); !slices.Equal(got, []string{"hosted", "prompt", "prompt-voice", "voice"}) {
		t.Errorf("agent kind enum = %v", got)
	}
}

func TestSchemaFilesAreValidJSON(t *testing.T) {
	err := fs.WalkDir(schemas.AzdFS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
			return err
		}
		b, rerr := fs.ReadFile(schemas.AzdFS(), p)
		if rerr != nil {
			return rerr
		}
		if !json.Valid(b) {
			t.Errorf("%s is not valid JSON", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestVendorDirectoryHasProvenanceAndLicence(t *testing.T) {
	dir := filepath.Join("..", "..", "schemas", "vendor", "azd")
	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"170ebb858071da353cc9bf8657a377bff268ba36", "a64ca8fe04d0bc1b0375c32fed1b76573e2a91ef", "MIT", "azure-dev-cli_1.35.0"} {
		if !strings.Contains(string(readme), want) {
			t.Errorf("README.md lacks %q", want)
		}
	}
	lic, err := os.ReadFile(filepath.Join(dir, "LICENSE"))
	if err != nil || !strings.Contains(string(lic), "Permission is hereby granted") || !strings.Contains(string(lic), "Microsoft Corporation") {
		t.Errorf("LICENSE missing or not the MIT text (err %v)", err)
	}
}
