package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLayoutProvider(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "svc"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "svc", "Dockerfile"), []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, closeFn := newLayoutProvider(Source{Dir: dir})
	defer closeFn()
	tests := []struct {
		name string
		got  bool
		want bool
	}{
		{"dir exists", l.DirExists("svc"), true},
		{"dir with dot prefix", l.DirExists("./svc"), true},
		{"file exists", l.FileExists("svc/Dockerfile"), true},
		{"missing dir", l.DirExists("nope"), false},
		{"file is not dir", l.DirExists("svc/Dockerfile"), false},
		{"dir is not file", l.FileExists("svc"), false},
		{"escape rejected", l.DirExists("../"), false},
		{"escape file rejected", l.FileExists("../../etc/passwd"), false},
		{"absolute rejected", l.FileExists(filepath.Join(dir, "svc", "Dockerfile")), false},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, tc.got, tc.want)
		}
	}
	nl, nclose := newLayoutProvider(Source{})
	defer nclose()
	if nl.DirExists("svc") || nl.FileExists("svc/Dockerfile") {
		t.Error("no Dir must report nothing exists")
	}
}

func TestEnvStoreRedactsSecretsAndSelects(t *testing.T) {
	dir := t.TempDir()
	e := filepath.Join(dir, ".azure", "dev")
	if err := os.MkdirAll(e, 0o750); err != nil {
		t.Fatal(err)
	}
	const sentinel = "s3cr3t-value-123"
	body := "AZURE_LOCATION=eastus\nAZURE_SUBSCRIPTION_ID=sub-1\nAZURE_RESOURCE_GROUP=rg-dev\n" +
		"MY_API_KEY=" + sentinel + "\nDB_CONNECTIONSTRING=\"" + sentinel + "\"\n# comment\nBROKEN LINE\n"
	if err := os.WriteFile(filepath.Join(e, ".env"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &envStore{src: Source{Dir: dir, Environment: "dev", Environments: []string{"dev", "prod"}}}
	vals := s.Values()
	if vals["AZURE_LOCATION"] != "eastus" {
		t.Errorf("location=%q", vals["AZURE_LOCATION"])
	}
	for _, k := range []string{"MY_API_KEY", "DB_CONNECTIONSTRING"} {
		v, ok := vals[k]
		if !ok || strings.Contains(v, sentinel) || !strings.HasPrefix(v, "sha256:") {
			t.Errorf("%s not digested: %q", k, v)
		}
	}
	if name, ok := s.Selected(); !ok || name != "dev" {
		t.Errorf("selected=%q,%v", name, ok)
	}
	envs := s.Environments()
	if len(envs) != 2 {
		t.Fatalf("envs=%v", envs)
	}
	for _, en := range envs {
		for _, v := range en.Values {
			if strings.Contains(v, sentinel) {
				t.Error("sentinel leaked through Environments")
			}
		}
	}
	if envs[0].Name == "dev" && (envs[0].Subscription != "sub-1" || envs[0].ResourceGroup != "rg-dev") {
		t.Errorf("dev env=%+v", envs[0])
	}
}

func TestEnvStoreMissingInputs(t *testing.T) {
	s := &envStore{src: Source{Dir: t.TempDir(), Environment: "ghost", Environments: []string{"dev"}}}
	if _, ok := s.Selected(); ok {
		t.Error("environment not in list must not be selected")
	}
	if len(s.Values()) != 0 {
		t.Error("missing .env must yield no values")
	}
	empty := &envStore{}
	if len(empty.Environments()) != 0 {
		t.Error("no Dir must yield no environments")
	}
}

func TestParseAzdVersion(t *testing.T) {
	tests := map[string]string{
		`{"azd":{"version":"1.34.2 (commit abc)"}}`: "1.34.2",
		"{\n  \"azd\": {\n    \"version\": \"1.34.2\",\n    \"commit\": \"04b2e1810e55c1981a574a84b018e92ba9f08f53\"\n  }\n}\n": "1.34.2",
		`{"version":"1.35.0-beta.1"}`:   "1.35.0-beta.1",
		`azd version 1.20.3 (commit x)`: "1.20.3",
		`{"azd":{}}`:                    "",
		``:                              "",
		`nonsense`:                      "",
	}
	for in, want := range tests {
		if got := parseAzdVersion([]byte(in)); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
