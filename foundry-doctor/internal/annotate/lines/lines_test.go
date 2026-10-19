package lines

import (
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		unicode bool
		lines   []string
		seps    []string
	}{
		{"empty", "", true, nil, nil},
		{"lf", "a\nb\n", false, []string{"a", "b"}, []string{"\n", "\n"}},
		{"no trailing break", "a\nb", false, []string{"a", "b"}, []string{"\n", ""}},
		{"crlf and lone cr", "a\r\nb\rc", false, []string{"a", "b", "c"}, []string{"\r\n", "\r", ""}},
		{"nel only when unicode", "a\u0085b", true, []string{"a", "b"}, []string{"\u0085", ""}},
		{"nel ignored otherwise", "a\u0085b", false, []string{"a\u0085b"}, []string{""}},
		{"ls and ps", "a b c", true, []string{"a", "b", "c"}, []string{" ", " ", ""}},
		{"blank line", "\n", false, []string{""}, []string{"\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, s := Split(tt.in, tt.unicode)
			if !reflect.DeepEqual(l, tt.lines) || !reflect.DeepEqual(s, tt.seps) {
				t.Fatalf("got %q %q want %q %q", l, s, tt.lines, tt.seps)
			}
		})
	}
}

func TestEOL(t *testing.T) {
	if EOL("a\r\nb") != "\r\n" || EOL("a\nb") != "\n" || EOL("") != "\n" || EOL("a\rb") != "\r" {
		t.Fatal("EOL")
	}
}
