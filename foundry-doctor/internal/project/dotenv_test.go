package project

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// The vectors were produced by running github.com/joho/godotenv v1.5.1
// Unmarshal on the same inputs; ParseDotEnv must give the same result.
func TestParseDotEnvMatchesGodotenv(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]string // nil means an error is expected
	}{
		{"plain", "A=1\nB=2\n", map[string]string{"A": "1", "B": "2"}},
		{"no trailing newline", "A=1", map[string]string{"A": "1"}},
		{"export prefix", "export A=1\nexport  B = 2", map[string]string{"A": "1", "B": "2"}},
		{"yaml style separator", "A: 1\nB:2", map[string]string{"A": "1", "B": "2"}},
		{"double quote escape", "A=\"x\\ny\"\n", map[string]string{"A": "x\ny"}},
		{"single quote literal", "A='x\\ny'\n", map[string]string{"A": `x\ny`}},
		{"multi-line double quote", "A=\"multi\nline\"\nB=2", map[string]string{"A": "multi\nline", "B": "2"}},
		{"inline comment needs space", "A=1 # c\nB=2#notc\n", map[string]string{"A": "1", "B": "2#notc"}},
		{"comment lines", "# c\n  # d\nA=1\n", map[string]string{"A": "1"}},
		{"unterminated quote", "A=\"unterminated\nB=2", nil},
		{"line without separator", "BROKEN LINE\nB=2", nil},
		{"crlf", "A=1\r\nB=2\r\n", map[string]string{"A": "1", "B": "2"}},
		{"duplicate key last wins", "A=1\nA=2\n", map[string]string{"A": "2"}},
		{"variable expansion", "A=x\nB=\"$A-${A}-\\$A\"\nC=$A\nD='$A'\n", map[string]string{"A": "x", "B": "x-x-$A", "C": "x", "D": "$A"}},
		{"bad key character", "A.B=1\nA-B=2\n", nil},
		{"BOM is not stripped", "\xef\xbb\xbfA=1\n", nil},
		{"empty value", "A=\n\nB=2", map[string]string{"A": "", "B": "2"}},
		{"spaced", "A =  spaced  \n", map[string]string{"A": "spaced"}},
		{"escaped quote", "A=\"q\\\"q\"\nB=2", map[string]string{"A": `q"q`, "B": "2"}},
		{"paren and unknown variable", "A=$(x)\nB=${NOPE}\nC=$\n", map[string]string{"A": "$(x)", "B": "", "C": "$"}},
		{"export without space is a key", "exportA=1\nexport=2\n", map[string]string{"exportA": "1", "export": "2"}},
		{"empty quotes", "A=\"\"\nB=''\n", map[string]string{"A": "", "B": ""}},
		{"quoted then comment", "A=\"a\" # c\nB=1", map[string]string{"A": "a", "B": "1"}},
		{"non-ASCII key byte rejected", "a=é\nÉ=1\n", nil},
		{"empty input", "", map[string]string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseDotEnv([]byte(tc.in))
			if tc.want == nil {
				if err == nil || !errors.Is(err, ErrDotEnv) {
					t.Fatalf("want ErrDotEnv, got %v %v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestParseDotEnvErrorsCarryNoValue(t *testing.T) {
	_, err := ParseDotEnv([]byte("API_KEY=\"sk-realsecretvalue\n"))
	if err == nil {
		t.Fatal("want error")
	}
	if got := err.Error(); strings.Contains(got, "sk-realsecretvalue") {
		t.Fatalf("error leaks value: %q", got)
	}
}
