package azdspike

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestStrictlyOneMappingDocument(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"mapping", "name: ok\n", ""},
		{"empty input", "", "empty document"},
		{"null root", "null\n", "root must be a mapping"},
		{"implicit null root", "~\n", "root must be a mapping"},
		{"scalar root", "hello\n", "root must be a mapping"},
		{"sequence root", "- name\n", "root must be a mapping"},
		{"second document", "name: one\n---\nname: two\n", "exactly one document"},
		{"empty second document", "name: one\n---\n", "exactly one document"},
		{"explicit empty second document", "name: one\n---\n...\n", "exactly one document"},
		{"malformed second document", "name: one\n---\nvalue: [unterminated\n", "invalid trailing YAML document"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := analyse([]byte(tc.src), nil)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("valid single mapping document rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestDuplicateEvidenceIsExactAndDeterministic(t *testing.T) {
	src := []byte(`name: first
name: second
services:
  café:
    host: azure.ai.project
    host: duplicate
  merged:
    <<: &first
      region: west
    <<: *first
    label: one
    label: two
z: 1
z: 2
`)
	want := []string{
		"name@1:1,2:1",
		"services.café.host@5:5,6:5",
		"services.merged.<<@8:5,10:5",
		"services.merged.label@11:5,12:5",
		"z@13:1,14:1",
	}
	for run := 0; run < 20; run++ {
		got, err := analyse(src, nil)
		if err != nil {
			t.Fatal(err)
		}
		var evidence []string
		for _, duplicate := range got.Duplicates {
			var locations []string
			for _, occurrence := range duplicate.Occurrences {
				locations = append(locations, fmt.Sprintf("%d:%d", occurrence.Line, occurrence.Column))
			}
			evidence = append(evidence, duplicate.Path+"@"+strings.Join(locations, ","))
		}
		if !slices.Equal(evidence, want) {
			t.Fatalf("run %d duplicate evidence = %v, want %v", run, evidence, want)
		}
	}
}

func TestUnicodeKeyColumnsCountCharacters(t *testing.T) {
	src := []byte("services:\n  café: {名: one, 名: two}\n")
	got, err := analyse(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Duplicates) != 1 {
		t.Fatalf("duplicates = %+v, want one", got.Duplicates)
	}
	duplicate := got.Duplicates[0]
	want := []position{{Line: 2, Column: 10}, {Line: 2, Column: 18}}
	if duplicate.Path != "services.café.名" || !slices.Equal(duplicate.Occurrences, want) {
		t.Fatalf("duplicate = %+v, want path services.café.名 at %+v", duplicate, want)
	}
}

// FuzzMalformedYAMLNeverPanics keeps malformed streams, document boundaries, aliases,
// duplicate keys, and non-mapping roots on the parser's ordinary error path.
func FuzzMalformedYAMLNeverPanics(f *testing.F) {
	for _, seed := range []string{
		"",
		"name: [",
		"name: ok\n---\n[",
		"name: ok\n---\n",
		"*missing\n",
		"&self [*self]\n",
		"{name: one, name: two}\n",
		"\xff\xfe\x00",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		// The assertion is absence of panic. Both a valid result and a syntax/shape
		// error are legitimate for arbitrary bytes.
		_, _ = analyse(src, nil)
	})
}
