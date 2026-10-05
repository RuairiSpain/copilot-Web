package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const canary = "CANARY-SECRET-9f3a7c"

func parseOK(t *testing.T, src string) EnvResult {
	t.Helper()
	res, err := ParseEnv(context.Background(), "dev", []byte(src))
	if err != nil {
		t.Fatalf("ParseEnv(%q): %v", src, err)
	}
	return res
}

func TestParseEnvGrammar(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want map[string]string
	}{
		{"simple", "A=1\nB=two\n", map[string]string{"A": "1", "B": "two"}},
		{"yaml style", "A: 1\nB:two\n", map[string]string{"A": "1", "B": "two"}},
		{"export prefix", "export A=1\nexport   B=2\n", map[string]string{"A": "1", "B": "2"}},
		{"export as key prefix without blank", "exportA=1\n", map[string]string{"exportA": "1"}},
		{"key chars", "a.b_C9=v\n", map[string]string{"a.b_C9": "v"}},
		{"comment lines", "# c\nA=1\n  # indented\nB=2\n", map[string]string{"A": "1", "B": "2"}},
		{"inline comment after blank", "A=1 # note\n", map[string]string{"A": "1"}},
		{"hash without blank stays", "A=1#frag\n", map[string]string{"A": "1#frag"}},
		{"leading hash value", "A=#x\n", map[string]string{"A": "#x"}},
		{"unquoted trimmed", "A=   spaced out   \n", map[string]string{"A": "spaced out"}},
		{"empty value", "A=\nB=2\n", map[string]string{"A": "", "B": "2"}},
		{"empty at eof", "A=", map[string]string{"A": ""}},
		{"no trailing newline", "A=1", map[string]string{"A": "1"}},
		{"single quote literal", "A='a\\nb $B'\n", map[string]string{"A": `a\nb $B`}},
		{"double quote escapes", "A=\"a\\nb\\rc\"\n", map[string]string{"A": "a\nb\rc"}},
		{"double quote other escape", `A="x\ty"` + "\n", map[string]string{"A": "xty"}},
		{"double quote escaped quote", `A="say \"hi\" now"` + "\n", map[string]string{"A": `say "hi" now`}},
		// godotenv trims every trailing quote character before unescaping, so an escaped quote
		// directly before the closing quote loses its quote and keeps a stray backslash.
		{"double quote escaped quote at end quirk", `A="say \"hi\""` + "\n", map[string]string{"A": `say "hi\`}},
		{"double quote multiline", "A=\"l1\nl2\"\nB=2\n", map[string]string{"A": "l1\nl2", "B": "2"}},
		{"quote hides hash", "A=\"x # y\"\n", map[string]string{"A": "x # y"}},
		{"expand earlier key", "A=1\nB=\"$A-${A}\"\n", map[string]string{"A": "1", "B": "1-1"}},
		{"expand forward ref empty", "B=\"$A\"\nA=1\n", map[string]string{"A": "1", "B": ""}},
		{"lowercase not expanded", "a=1\nB=\"$a\"\n", map[string]string{"a": "1", "B": "$a"}},
		{"escaped dollar", `A="\$X"` + "\n", map[string]string{"A": "$X"}},
		{"single quote no expand", "A=1\nB='$A'\n", map[string]string{"A": "1", "B": "$A"}},
		{"unquoted expands", "A=1\nB=$A\n", map[string]string{"A": "1", "B": "1"}},
		{"duplicate last wins", "A=1\nA=2\n", map[string]string{"A": "2"}},
		{"crlf", "A=1\r\nB=\"x\r\ny\"\r\n", map[string]string{"A": "1", "B": "x\ny"}},
		{"text after closing quote starts new statement", "A=\"x\" B=2\n", map[string]string{"A": "x", "B": "2"}},
		{"blank lines", "\n\n\nA=1\n\n\n", map[string]string{"A": "1"}},
		{"azd style sorted quoted", "AZURE_ENV_NAME=\"dev\"\nAZURE_LOCATION=\"eastus2\"\n", map[string]string{"AZURE_ENV_NAME": "dev", "AZURE_LOCATION": "eastus2"}},
		{"spaces around separator", "A = 1\n", map[string]string{"A": "1"}},
		{"empty file", "", map[string]string{}},
		{"only comments", "# a\n# b", map[string]string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := parseOK(t, tc.src)
			if got := len(res.Env.Keys()); got != len(tc.want) {
				t.Fatalf("keys %v, want %v", res.Env.Keys(), tc.want)
			}
			for k, w := range tc.want {
				v, ok := res.Env.Get(k)
				if !ok || v.Reveal() != w {
					t.Errorf("key %q = %q (present %v), want %q", k, v.Reveal(), ok, w)
				}
			}
			if res.Env.Name != "dev" {
				t.Errorf("name %q", res.Env.Name)
			}
		})
	}
}

func TestParseEnvErrors(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		line   int
		reason string
	}{
		{"unterminated double", "A=1\nB=\"abc\n", 2, ReasonUnterminated},
		{"unterminated single", "A='abc", 1, ReasonUnterminated},
		{"escaped final quote", `A="abc\"` + "\n", 1, ReasonUnterminated},
		{"bad key char", "A=1\nB-C=2\n", 2, ReasonBadKeyChar},
		{"bom", "\xef\xbb\xbfA=1\n", 1, ReasonBadKeyChar},
		{"no separator", "\n\nJUSTAKEY\n", 3, ReasonBadKeyChar},
		{"no separator at eof", "JUSTAKEY", 1, ReasonNoSeparator},
		{"empty key", "=value\n", 1, ReasonEmptyKey},
		{"binary", "A=1\nB=\x00\x01\n", 2, ReasonBinary},
		{"quote in key", "A\"=1\n", 1, ReasonBadKeyChar},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseEnv(context.Background(), "dev", []byte(tc.src))
			pe, ok := IsParseError(err)
			if !ok {
				t.Fatalf("want *ParseError, got %v", err)
			}
			if pe.Line != tc.line || pe.Reason != tc.reason {
				t.Fatalf("got line %d %q, want line %d %q", pe.Line, pe.Reason, tc.line, tc.reason)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("line %d", tc.line)) {
				t.Errorf("error lacks line: %v", err)
			}
		})
	}
	if _, ok := IsParseError(errors.New("x")); ok {
		t.Error("IsParseError true for a plain error")
	}
}

func TestParseEnvDuplicates(t *testing.T) {
	res := parseOK(t, "B=1\nA=1\nB=2\nA=3\nC=1\n")
	if strings.Join(res.DuplicateKeys, ",") != "A,B" {
		t.Fatalf("duplicates %v", res.DuplicateKeys)
	}
	if v, _ := res.Env.Get("B"); v.Reveal() != "2" {
		t.Fatalf("last duplicate must win, got %q", v.Reveal())
	}
	if len(parseOK(t, "A=1\n").DuplicateKeys) != 0 {
		t.Fatal("unexpected duplicates")
	}
}

// godotenv v1.5.1 scans an inline comment from the end of the line, so with two " #" markers the
// last one wins and the earlier text stays in the value. Pinned so a "fix" is a conscious choice.
func TestParseEnvInlineCommentQuirk(t *testing.T) {
	res := parseOK(t, "A=x # one # two\n")
	if v, _ := res.Env.Get("A"); v.Reveal() != "x # one" {
		t.Fatalf("got %q", v.Reveal())
	}
}

func TestParseEnvCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ParseEnv(ctx, "dev", []byte("A=1\n"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestParseEnvLarge(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&b, "KEY_%d=\"value %d\"\n", i, i)
	}
	res := parseOK(t, b.String())
	if n := len(res.Env.Keys()); n != 20000 {
		t.Fatalf("parsed %d keys", n)
	}
	one := parseOK(t, "BIG="+strings.Repeat("x", 1<<20)+"\n")
	if v, _ := one.Env.Get("BIG"); len(v.Reveal()) != 1<<20 {
		t.Fatal("long value truncated")
	}
}

// The canary is a secret value. It must not appear in any error text or any rendering of the
// parsed environment, however it is formatted.
func TestParseEnvNeverLeaksValues(t *testing.T) {
	good := parseOK(t, "TOKEN=\""+canary+"\"\nPLAIN="+canary+"\nSQ='"+canary+"'\n")
	j, err := json.Marshal(good.Env)
	if err != nil {
		t.Fatal(err)
	}
	renders := []string{
		good.Env.String(), good.Env.GoString(), string(j),
		fmt.Sprintf("%v %+v %#v %s", good.Env, good.Env, good.Env, good.Env),
	}
	for _, k := range good.Env.Keys() {
		v, _ := good.Env.Get(k)
		renders = append(renders, fmt.Sprintf("%v %+v %#v %s", v, v, v, v))
		vj, _ := json.Marshal(v)
		renders = append(renders, string(vj))
	}
	bad := []string{
		"TOKEN=\"" + canary + "\nB=1\n",
		"TOKEN-X=" + canary + "\n",
		canary,
		"TOKEN='" + canary,
		"\xef\xbb\xbfTOKEN=" + canary + "\n",
		"TOKEN=" + canary + "\x00\n",
		"=" + canary + "\n",
	}
	for _, s := range bad {
		_, err := ParseEnv(context.Background(), "dev", []byte(s))
		if err == nil {
			t.Fatalf("expected error for %q", s)
		}
		renders = append(renders, err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err))
	}
	for i, r := range renders {
		if strings.Contains(r, canary) {
			t.Errorf("render %d leaks the canary: %s", i, r)
		}
	}
}

func FuzzParseEnv(f *testing.F) {
	for _, s := range []string{"A=1", "A=\"x\\n$B\"", "export A: 'q'", "# c\nB=#", "A=\"", "\xef\xbb\xbfA=1", "A=1 # x # y"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		res, err := ParseEnv(context.Background(), "dev", data)
		if err != nil {
			if _, ok := IsParseError(err); !ok {
				t.Fatalf("non-parse error %v", err)
			}
			return
		}
		_ = res.Env.String()
	})
}
