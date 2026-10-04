#!/usr/bin/env python3
"""PreToolUse hook for Bash: block commands that mutate Azure, rewrite shared git history,
expose credentials, or run code piped from the network.

This is a speed bump for an agent that makes mistakes, not a security boundary. A command can
always be hidden inside a script or an interpreter (python, node) that this guard does not read.
The real boundaries are the permission rules in .claude/settings.json and the credentials the
session is given. See docs/development/agent-swarm.md.

Contract: reads the hook JSON on stdin. Exit 0 allows. Exit 2 blocks and prints the reason to
stderr. It fails closed: empty or malformed input, or a command it cannot parse, is blocked.
"""
import json
import os
import re
import shlex
import sys

MAX_DEPTH = 4

SHELLS = {"sh", "bash", "zsh", "dash", "ksh", "ash"}
INTERPRETERS = SHELLS | {"python", "python3", "node", "perl", "ruby", "php", "pwsh", "powershell"}
# Commands that run another command; the guard looks through them.
WRAPPERS = {"env", "command", "exec", "nohup", "time", "sudo", "doas", "timeout", "nice", "stdbuf", "xargs", "setsid", "builtin"}

AZ_READ_VERBS = {
    "show", "list", "get", "query", "version", "exists", "whoami", "list-locations", "list-skus",
    "list-usage", "list-usages", "list-versions", "list-deleted", "check-name", "check-name-availability",
}
AZ_ALLOWED_PREFIXES = {("bicep", "build"), ("bicep", "lint"), ("bicep", "version"), ("version",)}
SECRET_WORDS = {
    "keys", "secret", "secrets", "credential", "credentials", "get-access-token", "list-keys",
    "list-credentials", "connection-string", "list-connection-strings", "show-connection-string",
    "regenerate-key", "regenerate-keys",
}
AZD_ALLOWED_PREFIXES = {
    ("version",), ("show",), ("env", "list"), ("env", "get-value"), ("config", "show"), ("config", "list"),
    ("extension", "list"), ("extension", "show"), ("ai", "agent", "doctor"),
}

HOST_RE = re.compile(r"(azure\.com|azure\.net|windows\.net|microsoft\.com|microsoftonline\.com|azurewebsites\.net)", re.I)
CRED_BASENAME_RE = re.compile(
    r"^(\.env(\..+)?|.+\.(pem|key|pfx|p12|pkcs12|jks)|id_(rsa|dsa|ecdsa|ed25519)(\..+)?|"
    r"accessTokens\.json|msal_token_cache\.(json|bin)|azureProfile\.json|service_principal_entries\.json|credentials)$"
)
CRED_OK_BASENAMES = {".env.example", ".env.sample", ".env.template"}
PATH_ONLY_CMDS = {"ls", "stat", "file", "test", "[", "[["}


class Block(Exception):
    pass


def _strip_heredocs(script):
    """Remove heredoc markers and bodies. Returns (script, bodies for shell-fed heredocs)."""
    out, shell_bodies = [], []
    lines = script.split("\n")
    i = 0
    marker = re.compile(r"<<-?\s*(['\"]?)([A-Za-z_][A-Za-z0-9_]*)\1")
    while i < len(lines):
        line = lines[i]
        delims = [m.group(2) for m in marker.finditer(line)]
        if not delims:
            out.append(line)
            i += 1
            continue
        stripped = marker.sub("", line)
        out.append(stripped)
        i += 1
        for d in delims:
            body = []
            while i < len(lines) and lines[i].strip() != d:
                body.append(lines[i])
                i += 1
            i += 1  # the delimiter line
            if re.match(r"\s*(sudo\s+)?(sh|bash|zsh|dash)\b", stripped):
                shell_bodies.append("\n".join(body))
    return "\n".join(out), shell_bodies


def _newlines_to_semicolons(s):
    """Turn newlines outside quotes into ';' so each line is its own command."""
    out, quote, esc = [], None, False
    for ch in s:
        if esc:
            out.append(ch)
            esc = False
        elif ch == "\\" and quote != "'":
            out.append(ch)
            esc = True
        elif quote:
            out.append(ch)
            if ch == quote:
                quote = None
        elif ch in "'\"":
            out.append(ch)
            quote = ch
        elif ch == "\n":
            out.append(";")
        else:
            out.append(ch)
    return "".join(out)


def _substitutions(s):
    """Bodies of $(...) and `...` (innermost first, best effort)."""
    found = []
    prev = None
    while prev != s:
        prev = s
        for m in re.finditer(r"\$\(([^()]*)\)", s):
            found.append(m.group(1))
        s = re.sub(r"\$\(([^()]*)\)", " ", s)
    found += re.findall(r"`([^`]*)`", s)
    return found


def _split_segments(script):
    """Tokenise and split into command segments. Returns a list of (tokens, piped_in)."""
    lex = shlex.shlex(script, posix=True, punctuation_chars=True)
    lex.whitespace_split = True
    lex.commenters = ""
    segments, cur, piped, next_piped = [], [], False, False
    for tok in lex:
        if tok and set(tok) <= set(";&|()") and not set(tok) & set("<>"):
            if cur:
                segments.append((cur, piped))
            cur, piped = [], False
            next_piped = "|" in tok
            piped = next_piped
            continue
        if tok and ("<" in tok or ">" in tok) and set(tok) <= set("<>&|0123456789"):
            continue  # redirection operator; its target stays as an ordinary argument
        cur.append(tok)
    if cur:
        segments.append((cur, piped))
    return segments


def check(command, depth=0):
    """Raise Block with a reason, or return None if the command is allowed."""
    if depth > MAX_DEPTH:
        raise Block("command nesting is too deep to inspect")
    script, shell_bodies = _strip_heredocs(command)
    for body in shell_bodies:
        check(body, depth + 1)
    script = _newlines_to_semicolons(script)
    for sub in _substitutions(script):
        check(sub, depth + 1)
    try:
        segments = _split_segments(script)
    except ValueError as e:
        raise Block(f"cannot parse the command ({e}); simplify its quoting") from e
    for argv, piped in segments:
        _check_segment(argv, piped, depth)


def _unwrap(argv):
    """Drop env assignments and wrapper commands (env, sudo, timeout, xargs ...)."""
    while argv:
        head = argv[0]
        if re.match(r"^[A-Za-z_][A-Za-z0-9_]*=", head):
            argv = argv[1:]
            continue
        if os.path.basename(head) in WRAPPERS:
            argv = argv[1:]
            while argv and (argv[0].startswith("-") or re.match(r"^[A-Za-z_][A-Za-z0-9_]*=", argv[0]) or re.match(r"^\d+[smhd]?$", argv[0])):
                argv = argv[1:]
            continue
        break
    return argv


def _words_before_flag(args):
    words = []
    for a in args:
        if a.startswith("-"):
            break
        words.append(a)
    return words


def _check_segment(argv, piped, depth):
    argv = _unwrap(argv)
    if not argv:
        return
    cmd, args = argv[0], argv[1:]
    if cmd.startswith("$") or "$(" in cmd or "`" in cmd:
        raise Block("the command name comes from a variable or substitution and cannot be inspected")
    base = os.path.basename(cmd)

    if piped and base in INTERPRETERS and not any(a == "-c" for a in args):
        operand = next((a for a in args if not a.startswith("-") or a == "-"), None)
        reads_stdin = base in SHELLS and any(re.fullmatch(r"-[A-Za-z]*s[A-Za-z]*", a) for a in args)
        if operand is None or operand == "-" or reads_stdin:
            raise Block(f"piping into {base} with no script file runs unreviewed code from the pipe")

    if base in SHELLS:
        for i, a in enumerate(args):
            if a == "-c" and i + 1 < len(args):
                check(args[i + 1], depth + 1)
                break
    elif base == "eval":
        check(" ".join(args), depth + 1)
    elif base == "find":
        for i, a in enumerate(args):
            if a in ("-exec", "-execdir", "-ok", "-okdir"):
                end = next((j for j in range(i + 1, len(args)) if args[j] in (";", "+", "\\;")), len(args))
                _check_segment(args[i + 1:end], False, depth + 1)
    elif base == "az":
        _check_az(args)
    elif base == "azd":
        _check_azd(args)
    elif base == "git":
        _check_git(args)
    elif base in ("curl", "wget", "http", "https", "xh"):
        _check_http(base, args)

    _check_credential_paths(base, args)


def _check_az(args):
    words = _words_before_flag(args)
    flags = [a.lower() for a in args if a.startswith("-")]
    if not words:
        return  # az --version, az --help
    if words[0] == "rest":
        method = "get"
        for i, a in enumerate(args):
            if a in ("--method", "-m") and i + 1 < len(args):
                method = args[i + 1].lower()
            elif a.startswith("--method="):
                method = a.split("=", 1)[1].lower()
        if method not in ("get", "head") or any(f in ("--body", "-b") or f.startswith("--body=") for f in flags):
            raise Block("az rest is allowed only for GET/HEAD without a body")
        return
    if SECRET_WORDS & set(words):
        raise Block("this az command can print keys, secrets or tokens")
    if tuple(words[:2]) in AZ_ALLOWED_PREFIXES or tuple(words[:1]) in AZ_ALLOWED_PREFIXES:
        return
    if words[-1] in AZ_READ_VERBS:
        return
    raise Block(f"az {' '.join(words)} is not on the read-only allow-list (Foundry Doctor is read-only; ask the user)")


def _check_azd(args):
    words = _words_before_flag(args)
    if not words:
        return
    for n in range(len(words), 0, -1):
        if tuple(words[:n]) in AZD_ALLOWED_PREFIXES:
            return
    raise Block(f"azd {' '.join(words)} is not on the read-only allow-list (Foundry Doctor is read-only; ask the user)")


def _check_git(args):
    i = 0
    while i < len(args):
        a = args[i]
        if a in ("-c", "-C", "--git-dir", "--work-tree", "--namespace", "--exec-path") and i + 1 < len(args):
            i += 2
        elif a.startswith("-"):
            i += 1
        else:
            break
    if i >= len(args):
        return
    sub, rest = args[i], args[i + 1:]
    if sub == "push":
        for a in rest:
            if a.startswith("--force") or a in ("--delete", "--mirror", "--prune") or re.fullmatch(r"-[A-Za-z]*[fd][A-Za-z]*", a):
                raise Block("force, delete or mirror push")
            if not a.startswith("-") and (a.startswith("+") or a.startswith(":")):
                raise Block("push with a force (+) or delete (:) refspec")
    elif sub == "reset" and any(a == "--hard" for a in rest):
        raise Block("git reset --hard discards work")
    elif sub == "clean" and any(a == "--force" or re.fullmatch(r"-[A-Za-z]*f[A-Za-z]*", a) for a in rest):
        raise Block("git clean -f deletes untracked files")
    elif sub == "filter-branch":
        raise Block("git filter-branch rewrites history")


def _check_http(base, args):
    text = " ".join(args)
    if not HOST_RE.search(text):
        return
    mutating = False
    for i, a in enumerate(args):
        low = a.lower()
        if low in ("-x", "--request", "--method") and i + 1 < len(args) and args[i + 1].upper() not in ("GET", "HEAD"):
            mutating = True
        if low.startswith(("--request=", "--method=")) and low.split("=", 1)[1].upper() not in ("GET", "HEAD"):
            mutating = True
        if low in ("-d", "--data", "--data-raw", "--data-binary", "--data-urlencode", "-f", "--form", "-t", "--upload-file", "--json", "--post-data", "--post-file", "--body-data"):
            mutating = True
        if low.startswith(("--data", "--form", "--json=", "--post-")):
            mutating = True
    if mutating:
        raise Block("a non-GET request to an Azure or Microsoft endpoint")


def _check_credential_paths(base, args):
    if base in PATH_ONLY_CMDS or (base == "git" and args[:1] == ["check-ignore"]):
        return
    home_azure = os.path.expanduser("~/.azure")
    for a in args:
        if a.startswith("-") and "=" not in a:
            continue
        path = a.split("=", 1)[1] if a.startswith("-") else a
        if not path or path.startswith(("http://", "https://")):
            continue
        expanded = os.path.expanduser(path)
        name = os.path.basename(expanded.rstrip("/"))
        if name in CRED_OK_BASENAMES:
            continue
        if CRED_BASENAME_RE.match(name):
            raise Block(f"reads or copies a credential file ({name})")
        if expanded == home_azure or expanded.startswith(home_azure + "/"):
            raise Block("reads the Azure CLI profile directory")


def main():
    raw = sys.stdin.read()
    try:
        data = json.loads(raw)
    except ValueError:
        print("Blocked by guard_bash.py: hook input was empty or not valid JSON; refusing to run (fail closed).", file=sys.stderr)
        return 2
    command = (data.get("tool_input") or {}).get("command") if isinstance(data, dict) else None
    if not command:
        return 0  # no command to run
    try:
        check(command)
    except Block as e:
        print(f"Blocked by guard_bash.py: {e}. Foundry Doctor is read-only and never exposes credentials; ask the user if this is really intended.", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
