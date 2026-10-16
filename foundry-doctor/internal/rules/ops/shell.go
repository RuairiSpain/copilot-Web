package ops

import "strings"

// shellResult is the outcome of scanning one shell script for an azd
// provision, deploy or up invocation.
type shellResult int

const (
	// shellNone: the script was parsed and contains no such invocation.
	shellNone shellResult = iota
	// shellFound: azd provision|deploy|up is in command position in live code.
	shellFound
	// shellUncertain: the script uses a construct this scanner does not follow
	// (heredoc, function definition, unterminated quote, eval or sh -c, a
	// computed azd subcommand), so absence of a match proves nothing.
	shellUncertain
)

// shellWord is one word of a simple command with quoting removed.
type shellWord struct {
	text   string
	quoted bool // any part of the word was quoted
}

// scanShellForAzd tokenises script (POSIX shell, also tolerant of PowerShell
// for the constructs it recognises) and reports whether azd provision, deploy
// or up appears in command position: the first word of a command, where a
// command starts at the beginning of the text, a newline, `;`, `&`, `&&`, `||`,
// `|`, `(`, `$(` or a backtick. Text in comments, quotes and arguments such as
// `echo azd up` does not count. Statements under `if false`, `while false`,
// are dead and do not count.
func scanShellForAzd(script string) shellResult {
	s := &shellScanner{src: strings.ReplaceAll(strings.ReplaceAll(script, "\r\n", "\n"), "\\\n", "")}
	s.run()
	switch {
	case s.hard:
		return shellUncertain
	case s.found:
		return shellFound
	case s.soft:
		return shellUncertain
	}
	return shellNone
}

type block struct {
	kind string // if, loop, case
	dead bool
}

type shellScanner struct {
	src   string
	found bool
	hard  bool // an unfollowed construct that could hide or fake a match
	soft  bool // an unfollowed construct that could hide a match
	stack []block
}

func (s *shellScanner) run() {
	src := s.src
	var cur strings.Builder
	var curQuoted, inWord bool
	var words []shellWord
	endWord := func() {
		if inWord {
			words = append(words, shellWord{cur.String(), curQuoted})
		}
		cur.Reset()
		curQuoted, inWord = false, false
	}
	endCmd := func() {
		endWord()
		s.command(words)
		words = nil
	}
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '\'':
			j := strings.IndexByte(src[i+1:], '\'')
			if j < 0 {
				s.hard = true
				endCmd()
				return
			}
			cur.WriteString(src[i+1 : i+1+j])
			inWord, curQuoted = true, true
			i += j + 1
		case c == '"':
			j := i + 1
			var b strings.Builder
			closed := false
			for ; j < len(src); j++ {
				if src[j] == '\\' && j+1 < len(src) {
					b.WriteByte(src[j+1])
					j++
					continue
				}
				if src[j] == '"' {
					closed = true
					break
				}
				b.WriteByte(src[j])
			}
			if !closed {
				s.hard = true
				endCmd()
				return
			}
			if t := b.String(); strings.Contains(t, "$(") || strings.Contains(t, "`") {
				// A substitution inside a string may run azd.
				s.soft = true
			}
			cur.WriteString(b.String())
			inWord, curQuoted = true, true
			i = j
		case c == '\\' && i+1 < len(src):
			cur.WriteByte(src[i+1])
			inWord = true
			i++
		case c == '#' && !inWord:
			for i < len(src) && src[i] != '\n' {
				i++
			}
			i--
		case c == '<' && i+1 < len(src) && src[i+1] == '<':
			// Heredoc or here-string: the body is data we do not delimit.
			s.hard = true
			endCmd()
			return
		case c == '`':
			if i+1 < len(src) && src[i+1] == '\n' { // PowerShell continuation
				i++
				continue
			}
			endCmd()
		case c == '$' && i+1 < len(src) && src[i+1] == '(':
			endCmd()
			i++
		case c == '\n' || c == ';' || c == '(' || c == ')':
			if c == '(' {
				n := len(words)
				if inWord {
					n++
				}
				if n == 1 && strings.HasPrefix(strings.TrimLeft(src[i+1:], " \t"), ")") {
					s.hard = true // name() function definition
				}
			}
			endCmd()
		case c == '&' || c == '|':
			if i+1 < len(src) && src[i+1] == c {
				i++
			}
			endCmd()
		case c == ' ' || c == '\t':
			endWord()
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	endCmd()
}

func (s *shellScanner) live() bool {
	for _, b := range s.stack {
		if b.dead {
			return false
		}
	}
	return true
}

var shellPrefixWords = map[string]bool{
	"then": true, "do": true, "else": true, "{": true, "!": true,
	"time": true, "exec": true, "command": true, "sudo": true, "nohup": true, "env": true,
}

func isAssignment(w shellWord) bool {
	if w.quoted {
		return false
	}
	i := strings.IndexByte(w.text, '=')
	if i <= 0 {
		return false
	}
	for j := 0; j < i; j++ {
		c := w.text[j]
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || j > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func isFalse(ws []shellWord) bool {
	if len(ws) == 0 {
		return false
	}
	t := func(i int) string {
		if i < len(ws) {
			return ws[i].text
		}
		return ""
	}
	return len(ws) == 1 && (t(0) == "false" || t(0) == "/bin/false") ||
		len(ws) == 2 && t(0) == "!" && (t(1) == "true" || t(1) == "/bin/true")
}

// command interprets one simple command (a list of words).
func (s *shellScanner) command(ws []shellWord) {
	for len(ws) > 0 {
		w := ws[0]
		switch {
		case !w.quoted && (w.text == "else" || w.text == "elif"):
			rest := ws[1:]
			if n := len(s.stack); n > 0 && s.stack[n-1].kind == "if" {
				s.stack[n-1].dead = w.text == "elif" && isFalse(rest)
			}
			ws = rest
			continue
		case !w.quoted && shellPrefixWords[w.text], isAssignment(w):
			ws = ws[1:]
			continue
		}
		break
	}
	if len(ws) == 0 {
		return
	}
	head := ws[0].text
	if ws[0].quoted {
		return
	}
	switch head {
	case "if", "while", "until":
		kind := "if"
		if head != "if" {
			kind = "loop"
		}
		s.stack = append(s.stack, block{kind: kind, dead: isFalse(ws[1:]) && head != "until"})
		s.scanAfter(ws[1:])
		return
	case "for":
		s.stack = append(s.stack, block{kind: "loop"})
		return
	case "case":
		s.stack = append(s.stack, block{kind: "case"})
		s.soft = true
		return
	case "fi", "done", "esac":
		if n := len(s.stack); n > 0 {
			s.stack = s.stack[:n-1]
		}
		return
	case "function":
		s.hard = true
		return
	case "eval", "bash", "sh", "zsh", "pwsh", "powershell":
		for _, w := range ws[1:] {
			if w.text == "-c" || w.text == "-Command" || w.text == "-command" || head == "eval" {
				s.soft = true
			}
		}
		return
	}
	s.simple(ws)
}

// scanAfter handles the condition of an if/while: it is itself a command.
func (s *shellScanner) scanAfter(ws []shellWord) {
	if len(ws) > 0 {
		s.command(ws)
	}
}

func (s *shellScanner) simple(ws []shellWord) {
	first := ws[0].text
	if first != "azd" && first != "azd.exe" {
		return
	}
	if !s.live() {
		return
	}
	if len(ws) < 2 {
		return
	}
	sub := ws[1]
	switch strings.ToLower(sub.text) {
	case "provision", "deploy", "up":
		if !sub.quoted {
			s.found = true
		}
		return
	}
	if strings.HasPrefix(sub.text, "-") || strings.ContainsAny(sub.text, "$") {
		// A global flag with a separate value, or a computed subcommand.
		s.soft = true
	}
}
