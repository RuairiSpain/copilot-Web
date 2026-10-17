package bicep

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

// ReasonFileLoadOutsideProject is the named skip reason when Bicep source calls
// a file-loading function in a way the doctor cannot prove stays under the
// project root (ADR-018 point 7).
const ReasonFileLoadOutsideProject = "bicep-file-load-outside-project"

// Bounds on the pre-compile source scan.
const (
	maxGuardFiles     = 256
	maxGuardFileBytes = 1 << 20
)

// loadFunctions are the Bicep functions that read a file at compile time
// (lower case: Bicep function names are matched case-insensitively here).
var loadFunctions = map[string]bool{
	"loadtextcontent":       true,
	"loadfileasbase64":      true,
	"loadjsoncontent":       true,
	"loadyamlcontent":       true,
	"loaddirectoryfileinfo": true,
}

// LoadGuardError is the refusal to compile a Bicep or .bicepparam file because
// it, or a local file it references, may read outside the project root.
type LoadGuardError struct {
	File   string // project-relative file holding the call
	Detail string
}

func (e *LoadGuardError) Error() string {
	return fmt.Sprintf("%s: %s in %s", ReasonFileLoadOutsideProject, e.Detail, e.File)
}

// GuardFileLoads scans start (a slash-separated path relative to root) and
// every local file it references through a module, using, extends or
// import ... from statement (any extension; a .json file is checked for
// location only), transitively
// and bounded, for calls to loadTextContent, loadFileAsBase64, loadJsonContent,
// loadYamlContent and loadDirectoryFileInfo. A call is allowed only when its
// first argument is a plain single-quoted literal naming a relative path that
// resolves, through root, to a regular file (a directory for
// loadDirectoryFileInfo) inside the project. Anything else, including a
// non-literal argument, an absolute or escaping path, a symbolic link or a
// Windows junction or other reparse point reported as an irregular file on the
// path, a referenced file outside the root or a bound being exceeded, returns a
// *LoadGuardError. It is a lexical scan, not a Bicep parser: comments and
// string literals are skipped, and when in doubt the file is refused.
func GuardFileLoads(root *os.Root, start string) error {
	first := path.Clean(start)
	queue := []string{first}
	seen := map[string]bool{first: true}
	for len(queue) > 0 {
		file := queue[0]
		queue = queue[1:]
		src, err := readBounded(root, file)
		if err != nil {
			if file == first {
				return &LoadGuardError{File: file, Detail: "source could not be read for the file-load scan"}
			}
			if errors.Is(err, fs.ErrNotExist) {
				continue // a missing module is the compiler's diagnostic
			}
			// Any other failure (a link leaving the root, a permission error,
			// a directory, an oversize file) means the file may be read by the
			// compiler but not by the scan.
			return &LoadGuardError{File: file, Detail: "referenced file could not be read for the file-load scan"}
		}
		if strings.HasSuffix(strings.ToLower(file), ".json") {
			continue // an ARM JSON module cannot call load functions; only its location is checked
		}
		sc := scanSource(src)
		if sc.doubt != "" {
			return &LoadGuardError{File: file, Detail: sc.doubt}
		}
		dir := path.Dir(file)
		for _, c := range sc.calls {
			if !c.isLiteral {
				return &LoadGuardError{File: file, Detail: c.fn + " with a path that is not a plain string literal"}
			}
			rel, ok := projectRel(dir, c.literal)
			if !ok {
				return &LoadGuardError{File: file, Detail: c.fn + " path is absolute or leaves the project root"}
			}
			fi, err := root.Stat(rel)
			if err != nil {
				return &LoadGuardError{File: file, Detail: c.fn + " path does not resolve to a file inside the project root"}
			}
			if sensitiveDir(rel) != "" {
				return &LoadGuardError{File: file, Detail: c.fn + " path is inside the " + sensitiveDir(rel) + " directory, which is not a source of template content"}
			}
			if hasSymlink(root, rel) {
				return &LoadGuardError{File: file, Detail: c.fn + " path passes through a symbolic link"}
			}
			if strings.EqualFold(c.fn, "loadDirectoryFileInfo") {
				if !fi.IsDir() {
					return &LoadGuardError{File: file, Detail: c.fn + " path is not a directory inside the project root"}
				}
			} else if !fi.Mode().IsRegular() {
				return &LoadGuardError{File: file, Detail: c.fn + " path is not a regular file inside the project root"}
			}
		}
		for _, ref := range sc.refs {
			rel, ok := projectRel(dir, ref)
			if !ok {
				return &LoadGuardError{File: file, Detail: "referenced Bicep, Bicep parameter or JSON module file is absolute or leaves the project root"}
			}
			if hasSymlink(root, rel) {
				return &LoadGuardError{File: file, Detail: "referenced file path passes through a symbolic link"}
			}
			if seen[rel] {
				continue
			}
			if len(seen) >= maxGuardFiles {
				return &LoadGuardError{File: file, Detail: "too many referenced Bicep files to scan"}
			}
			seen[rel] = true
			queue = append(queue, rel)
		}
	}
	return nil
}

// projectRel joins a literal path onto dir and reports whether the cleaned
// result is a relative path that stays inside the root.
func projectRel(dir, lit string) (string, bool) {
	if lit == "" || strings.ContainsAny(lit, "\\:\x00") || strings.HasPrefix(lit, "/") {
		return "", false
	}
	rel := path.Join(dir, lit)
	if rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// sensitiveDir returns ".azure" or ".git" when a root-relative slash path has
// such a segment (any case), else "". azd keeps environment values in .azure
// and a repository keeps its history and config in .git; loads from them are
// refused.
func sensitiveDir(rel string) string {
	for _, seg := range strings.Split(rel, "/") {
		for _, d := range []string{".azure", ".git"} {
			if strings.EqualFold(seg, d) {
				return d
			}
		}
	}
	return ""
}

// hasSymlink reports whether any component of the root-relative path is a
// symbolic link, a Windows junction or other reparse point that Go reports as
// an irregular file, or cannot be examined; a component that does not exist
// ends the walk with false. It is used for load targets and for module, using,
// extends and import references. A link inside the project could point at
// .azure or .git, which the text check cannot see, so loads through links are
// refused.
//
// Since Go 1.23 on Windows, os.Lstat (and Root.Lstat) report a symbolic link as
// os.ModeSymlink but a junction (mount point) and most other reparse points as
// os.ModeIrregular only (os/types_windows.go, fileStat.mode); both are refused.
// On other systems the Go runtime does not set os.ModeIrregular for sockets,
// devices or pipes (they have their own mode bits), so the extra bit does not
// refuse them here; a non-regular load target is refused later anyway. Reparse
// points that Go reports as regular (for example Data Deduplication files) are
// not examined.
func hasSymlink(root *os.Root, rel string) bool {
	parts := strings.Split(rel, "/")
	for i := range parts {
		fi, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if errors.Is(err, fs.ErrNotExist) {
			return false // nothing further to follow; existence is checked by the caller
		}
		if err != nil || fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return true
		}
	}
	return false
}

// isRegistryRef reports whether a lower-cased module reference names a Bicep
// registry (br:, br/) or template spec (ts:) source, not a local file.
func isRegistryRef(l string) bool {
	return strings.HasPrefix(l, "br:") || strings.HasPrefix(l, "br/") || strings.HasPrefix(l, "ts:")
}

func readBounded(root *os.Root, rel string) ([]byte, error) {
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxGuardFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxGuardFileBytes {
		return nil, errors.New("too large")
	}
	return data, nil
}

type loadCall struct {
	fn        string
	literal   string
	isLiteral bool
}

type scanResult struct {
	calls []loadCall
	refs  []string
	doubt string
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentPart(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// scanSource finds load-function calls and local file references in the code
// parts of src. References are collected only from module, using, extends and
// import ... from statements; other strings that look like paths are ignored. Comments and the text of string literals are skipped;
// ${...} interpolations inside strings are code and are scanned.
func scanSource(src []byte) scanResult {
	var res scanResult
	n := len(src)
	// interp holds, per open string interpolation, the brace depth inside it.
	var interp []int
	var lit []byte
	hasInterp := false
	inStr := false
	// t1 and t2 are the last two code tokens before the current one: a
	// lower-cased identifier, or tokNone for punctuation and strings.
	const tokNone = "\x00"
	t1, t2 := tokNone, tokNone
	push := func(t string) { t2, t1 = t1, t }
	i := 0
	for i < n {
		c := src[i]
		if inStr {
			switch {
			case c == '\\':
				lit = append(lit, c)
				i++
				if i < n && src[i] != '\n' {
					lit = append(lit, src[i])
					i++
				}
			case c == '\n':
				inStr = false
			case c == '$' && i+1 < n && src[i+1] == '{':
				hasInterp = true
				i += 2
				interp = append(interp, 0)
				inStr = false
			case c == '\'':
				i++
				inStr = false
				if !hasInterp {
					s := string(lit)
					l := strings.ToLower(s)
					// Bicep compiles a module file of any extension as Bicep
					// source, so every literal in a reference position is a
					// local file unless it is a registry reference.
					if (t1 == "using" || t1 == "extends" || t1 == "from" || t2 == "module") && !isRegistryRef(l) {
						res.refs = append(res.refs, s)
					}
				}
				push(tokNone)
			default:
				lit = append(lit, c)
				i++
			}
			continue
		}
		switch {
		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && src[i+1] == '*':
			j := bytes.Index(src[i+2:], []byte("*/"))
			if j < 0 {
				res.doubt = "unterminated block comment"
				return res
			}
			i += 2 + j + 2
		case c == '\'' && i+2 < n && src[i+1] == '\'' && src[i+2] == '\'':
			j := bytes.Index(src[i+3:], []byte("'''"))
			if j < 0 {
				res.doubt = "unterminated multi-line string"
				return res
			}
			i += 3 + j + 3
			push(tokNone)
		case c == '\'':
			i++
			lit = lit[:0]
			hasInterp = false
			inStr = true
		case c == '{' && len(interp) > 0:
			interp[len(interp)-1]++
			i++
		case c == '}' && len(interp) > 0:
			i++
			if interp[len(interp)-1] == 0 {
				interp = interp[:len(interp)-1]
				// resume the enclosing string; it is not a plain literal
				lit = lit[:0]
				hasInterp = true
				inStr = true
			} else {
				interp[len(interp)-1]--
			}
		case isIdentStart(c):
			j := i
			for j < n && isIdentPart(src[j]) {
				j++
			}
			name := string(src[i:j])
			i = j
			lname := strings.ToLower(name)
			push(lname)
			if loadFunctions[lname] {
				k, ok := skipSpaceAndBlockComments(src, j)
				switch {
				case !ok:
					res.doubt = "unterminated block comment after " + name
					return res
				case k < n && src[k] == '(':
					res.calls = append(res.calls, firstArg(name, src, k+1))
				default:
					res.doubt = name + " is not directly followed by '('"
					return res
				}
			}
		default:
			if !isSpace(c) {
				push(tokNone)
			}
			i++
		}
	}
	if len(interp) > 0 {
		res.doubt = "unterminated string interpolation"
	}
	return res
}

// skipSpaceAndBlockComments advances k over whitespace and /* */ comments,
// which Bicep accepts between a function name and its '('. It returns false
// when a block comment is not terminated.
func skipSpaceAndBlockComments(src []byte, k int) (int, bool) {
	n := len(src)
	for k < n {
		switch {
		case isSpace(src[k]):
			k++
		case src[k] == '/' && k+1 < n && src[k+1] == '*':
			j := bytes.Index(src[k+2:], []byte("*/"))
			if j < 0 {
				return k, false
			}
			k += 2 + j + 2
		default:
			return k, true
		}
	}
	return k, true
}

// firstArg reads the first argument after '(' at src[k:]. It is a plain
// literal only when it is one single-quoted string without escapes or
// interpolation, followed by ',' or ')'.
func firstArg(fn string, src []byte, k int) loadCall {
	c := loadCall{fn: fn}
	n := len(src)
	for k < n && isSpace(src[k]) {
		k++
	}
	if k >= n || src[k] != '\'' || (k+2 < n && src[k+1] == '\'' && src[k+2] == '\'') {
		return c
	}
	k++
	start := k
	for k < n && src[k] != '\'' {
		if src[k] == '\\' || src[k] == '\n' || (src[k] == '$' && k+1 < n && src[k+1] == '{') {
			return c
		}
		k++
	}
	if k >= n {
		return c
	}
	lit := string(src[start:k])
	k++
	for k < n && isSpace(src[k]) {
		k++
	}
	if k >= n || (src[k] != ',' && src[k] != ')') {
		return c
	}
	c.literal, c.isLiteral = lit, true
	return c
}
