package azureyaml

import (
	"errors"
	"strings"
	"testing"
)

const sample = `name: demo
services:
  web:
    host: containerapp
    uses: [db, ghost]
  api:
    host: containerapp
resources:
  db:
    type: db.postgres
`

func TestParseValid(t *testing.T) {
	d, err := Parse([]byte(sample), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(d.ServiceNames(), ","); got != "api,web" {
		t.Errorf("services = %s", got)
	}
	if got := strings.Join(d.ResourceNames(), ","); got != "db" {
		t.Errorf("resources = %s", got)
	}
	v, loc, ok := d.Lookup("services", "web", "host")
	if !ok || v != "containerapp" || loc.Line != 4 || loc.Column != 11 || loc.File != "azure.yaml" {
		t.Errorf("lookup = %v %+v %v", v, loc, ok)
	}
	if _, _, ok := d.Lookup("services", "nope"); ok {
		t.Error("missing path found")
	}
	diags := d.Diagnostics()
	if len(diags) != 1 || !strings.Contains(diags[0].Message, `"ghost"`) || diags[0].Location.Line != 5 {
		t.Errorf("diagnostics = %+v", diags)
	}
}

func TestParseDuplicateKeys(t *testing.T) {
	d, err := Parse([]byte("name: a\nname: b\nservices:\n  x:\n    host: a\n    host: b\n"), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	diags := d.Diagnostics()
	if len(diags) != 2 || diags[0].Location.Line != 2 || diags[1].Location.Line != 6 {
		t.Errorf("diagnostics = %+v", diags)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name, in string
		empty    bool
	}{
		{"empty", "", true},
		{"syntax", "a: [1, 2\n", false},
		{"scalar root", "hello\n", false},
		{"list root", "- a\n", false},
		{"multi doc", "a: 1\n---\nb: 2\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.in), "azure.yaml")
			if err == nil {
				t.Fatal("expected error")
			}
			if tt.empty != errors.Is(err, ErrEmpty) {
				t.Errorf("ErrEmpty mismatch: %v", err)
			}
		})
	}
}

func TestLookupDoesNotLeakNodes(t *testing.T) {
	d, _ := Parse([]byte("a:\n  b: [1, 2]\n"), "x")
	v, _, ok := d.LookupAny("a", "b")
	if !ok {
		t.Fatal("not found")
	}
	if l, isList := v.([]any); !isList || len(l) != 2 {
		t.Errorf("v = %#v", v)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(sample))
	f.Add([]byte("a: &x {b: 1}\nc: *x\n"))
	f.Add([]byte("a: [\n"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := Parse(data, "azure.yaml")
		if err != nil {
			return
		}
		_ = d.Diagnostics()
		_ = d.ServiceNames()
		d.Lookup("services")
	})
}
