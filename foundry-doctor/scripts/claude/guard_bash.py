#!/usr/bin/env python3
"""PreToolUse hook for Bash: block commands that mutate Azure, rewrite shared git history,
expose credentials, or run code piped from the network.

This is a speed bump for an agent that makes mistakes, not a security boundary. A command can
always be hidden inside a script or an interpreter (python, node, awk) that this guard does not read.
The real boundary is the credential the session holds: run agent sessions with a read-only or no
Azure identity and keep AZURE_*, GITHUB_TOKEN and login state out of the environment.
See docs/development/agent-swarm.md.

Contract: reads the hook JSON on stdin. Exit 0 allows. Exit 2 blocks and prints the reason to
stderr. It fails closed: empty or malformed input, or a command it cannot parse or classify, is blocked.
"""
import glob
import json
import os
import re
import shlex
import sys

MAX_DEPTH = 4

SHELLS = {"sh", "bash", "zsh", "dash", "ksh", "ash"}
INTERPRETERS = SHELLS | {"python", "python3", "node", "perl", "ruby", "php", "pwsh", "powershell"}

# Keywords that can precede a command in a compound statement.
KEYWORDS = {"{", "}", "!", "then", "do", "else", "elif", "if", "while", "until", "coproc"}

# Wrapper commands that run another command, and the options of each that consume a following argument.
WRAPPER_ARG_OPTS = {
    "env": {"-u", "--unset", "-C", "--chdir", "-S", "--split-string"},
    "command": set(),
    "exec": {"-a"},
    "nohup": set(),
    "builtin": set(),
    "setsid": set(),
    "time": {"-f", "--format", "-o", "--output"},  # -p takes no argument
    "sudo": {"-u", "--user", "-g", "--group", "-h", "--host", "-C", "--close-from", "-p", "--prompt", "-r", "--role",
             "-t", "--type", "-D", "--chdir", "-R", "--chroot", "-T", "--command-timeout"},
    "doas": {"-u", "-C"},
    "timeout": {"-s", "--signal", "-k", "--kill-after"},
    "nice": {"-n", "--adjustment"},
    "stdbuf": {"-i", "-o", "-e", "--input", "--output", "--error"},
    "xargs": {"-n", "-P", "-I", "-i", "-d", "-L", "-s", "-E", "-a", "--max-args", "--max-procs", "--delimiter",
              "--max-lines", "--max-chars", "--arg-file", "--replace", "--eof"},
}
# Wrappers whose first positional argument is not the command.
WRAPPER_SKIP_POSITIONAL = {"timeout": 1}

# Commands that launch other commands in ways this guard does not model. They are blocked outright.
OPAQUE_LAUNCHERS = {"busybox", "su", "flock", "ssh", "watch", "strace", "ltrace", "script", "parallel", "tmux", "screen",
                    "taskset", "ionice", "chroot", "nsenter", "unshare", "runuser", "at", "batch", "crontab", "systemd-run",
                    "docker", "podman", "kubectl", "nerdctl"}

AZ_GLOBAL_VALUE_FLAGS = {"--subscription", "--output", "-o", "--query", "--query-expression"}
AZ_GLOBAL_BOOL_FLAGS = {"--debug", "--verbose", "--only-show-errors", "--help", "-h", "--version"}
AZD_GLOBAL_VALUE_FLAGS = {"-C", "--cwd", "-e", "--environment", "-o", "--output", "--trace-log-file", "--trace-log-url"}
AZD_GLOBAL_BOOL_FLAGS = {"--debug", "--no-prompt", "--help", "-h", "--docs", "--version"}

AZ_READ_GROUPS = {
    "account", "group", "resource", "graph", "cognitiveservices", "search", "storage", "keyvault", "role", "policy", "provider",
    "network", "monitor", "apim", "cosmosdb", "ml", "bicep", "lock", "deployment", "tag", "containerapp", "acr", "identity", "ad",
    "security", "advisor", "consumption", "costmanagement", "vm", "aks", "webapp", "functionapp", "sql", "redis", "servicebus",
    "eventhubs", "appconfig", "databricks", "synapse", "datafactory", "extension", "feature", "version",
}
AZ_READ_VERBS = {
    "show", "list", "get", "query", "version", "exists", "whoami", "list-locations", "list-skus",
    "list-usage", "list-usages", "list-versions", "list-deleted", "check-name", "check-name-availability",
}
AZ_ALLOWED_PREFIXES = {("bicep", "build"), ("bicep", "lint"), ("bicep", "version"), ("version",)}
SECRET_WORDS = {
    "keys", "secret", "secrets", "credential", "credentials", "get-access-token", "list-keys",
    "list-credentials", "connection-string", "list-connection-strings", "show-connection-string",
    "regenerate-key", "regenerate-keys", "admin-key", "query-key", "appsettings", "app-insights", "publishing-credentials",
    "token", "access-token", "sas", "generate-sas",
}
AZD_ALLOWED_PREFIXES = {
    ("version",), ("show",), ("env", "list"), ("env", "get-value"), ("config", "show"), ("config", "list"),
    ("extension", "list"), ("extension", "show"), ("ai", "agent", "doctor"),
}

HOST_RE = re.compile(r"(azure\.com|azure\.net|windows\.net|microsoft\.com|microsoftonline\.com|azurewebsites\.net)", re.I)
CRED_BASENAME_RE = re.compile(
    r"^(\.env(\..+)?|.+\.(pem|key|pfx|p12|pkcs12|jks)|id_(rsa|dsa|ecdsa|ed25519)(\..+)?|\.netrc|_netrc|\.git-credentials|"
    r"\.npmrc|\.pypirc|accessTokens\.json|msal_token_cache\.(json|bin)|azureProfile\.json|service_principal_entries\.json|"
    r"credentials|environ)$"
)
CRED_OK_BASENAMES = {".env.example", ".env.sample", ".env.template"}
CRED_DIRS = ["~/.azure", "~/.aws", "~/.config/gh", "~/.kube", "~/.ssh", "~/.docker", "~/.gnupg", "~/.config/gcloud"]
SECRET_VAR_RE = re.compile(r"\$\{?[A-Za-z_]*(SECRET|TOKEN|PASSWORD|PASSWD|CREDENTIAL|API_?KEY|ACCESS_?KEY|PRIVATE_?KEY)[A-Za-z_]*\}?", re.I)
PATH_ONLY_CMDS = {"ls", "stat", "file", "test", "[", "[["}
ENV_DUMPERS = {"printenv", "declare", "typeset"}
GIT_DANGEROUS_CONFIG = ("core.pager", "core.editor", "core.sshcommand", "core.fsmonitor", "core.hookspath", "credential.helper",
                        "core.askpass", "diff.external", "gpg.program")


class Block(Exception):
    pass


def _strip_heredocs(script):
    """Remove heredoc markers and bodies.

    Returns (script, shell_bodies, expanding_bodies): bodies fed to a shell, and bodies of heredocs with an unquoted
    delimiter, in which the shell still runs $(...) and backticks.
    """
    out, shell_bodies, expanding = [], [], []
    lines = script.split("\n")
    i = 0
    marker = re.compile(r"(?<!<)<<-?\s*(['\"]?)([A-Za-z_][A-Za-z0-9_]*)\1(?!<)")
    while i < len(lines):
        line = lines[i]
        found = [(m.group(2), m.group(1) == "") for m in marker.finditer(line)]
        delims = [d for d, _ in found]
        if not delims:
            out.append(line)
            i += 1
            continue
        stripped = marker.sub("", line)
        out.append(stripped)
        i += 1
        for d, unquoted in found:
            body = []
            while i < len(lines) and lines[i].strip() != d:
                body.append(lines[i])
                i += 1
            i += 1  # the delimiter line
            if re.match(r"\s*(sudo\s+)?(sh|bash|zsh|dash)\b", stripped):
                shell_bodies.append("\n".join(body))
            elif unquoted:
                expanding.append("\n".join(body))
    return "\n".join(out), shell_bodies, expanding


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
    found += re.findall(r"[<>]\(([^()]*)\)", s)  # process substitution
    return found


def _split_segments(script):
    """Tokenise and split into command segments: (tokens, piped_in, here_strings)."""
    lex = shlex.shlex(script, posix=True, punctuation_chars=True)
    lex.whitespace_split = True
    lex.commenters = ""
    toks = list(lex)
    segments, cur, here, piped = [], [], [], False
    i = 0
    while i < len(toks):
        tok = toks[i]
        i += 1
        if tok == "<<<":
            if i < len(toks):
                here.append(toks[i])
                i += 1
            continue
        if tok and set(tok) <= set(";&|()") and not set(tok) & set("<>"):
            if cur:
                segments.append((cur, piped, here))
            cur, here = [], []
            piped = "|" in tok
            continue
        if tok and ("<" in tok or ">" in tok) and set(tok) <= set("<>&|0123456789"):
            continue  # redirection operator; its target stays as an ordinary argument
        cur.append(tok)
    if cur:
        segments.append((cur, piped, here))
    return segments


def check(command, depth=0):
    """Raise Block with a reason, or return None if the command is allowed."""
    if depth > MAX_DEPTH:
        raise Block("command nesting is too deep to inspect")
    script, shell_bodies, expanding = _strip_heredocs(command)
    for body in shell_bodies:
        check(body, depth + 1)
    for body in expanding:
        for sub_cmd in _substitutions(body):
            check(sub_cmd, depth + 1)
    script = _newlines_to_semicolons(script)
    for sub in _substitutions(script):
        check(sub, depth + 1)
    try:
        segments = _split_segments(script)
    except ValueError as e:
        raise Block(f"cannot parse the command ({e}); simplify its quoting") from e
    for argv, piped, here in segments:
        _check_segment(argv, piped, here, depth)


def _is_assignment(tok):
    return re.match(r"^[A-Za-z_][A-Za-z0-9_]*=", tok) is not None


def _unwrap(argv, depth):
    """Drop env assignments, shell keywords and wrapper commands (with their options). Returns the real argv."""
    while argv:
        head = argv[0]
        base = os.path.basename(head)
        if _is_assignment(head) or head in KEYWORDS:
            argv = argv[1:]
            continue
        if head == "function" and len(argv) >= 2:
            argv = argv[2:]  # "function name" then the body
            continue
        if base in WRAPPER_ARG_OPTS:
            argv = argv[1:]
            takes_arg = WRAPPER_ARG_OPTS[base]
            skip_pos = WRAPPER_SKIP_POSITIONAL.get(base, 0)
            while argv:
                tok = argv[0]
                if tok == "--":
                    argv = argv[1:]
                    break
                if tok.startswith("-") and tok != "-":
                    if base == "env" and tok in ("-S", "--split-string") and len(argv) > 1:
                        check(argv[1], depth + 1)
                    if tok.startswith("--split-string="):
                        check(tok.split("=", 1)[1], depth + 1)
                    consumed = 2 if (tok in takes_arg and "=" not in tok) else 1
                    argv = argv[consumed:]
                    continue
                if base == "env" and _is_assignment(tok):
                    argv = argv[1:]
                    continue
                if skip_pos > 0:
                    argv = argv[1:]
                    skip_pos -= 1
                    continue
                break
            continue
        break
    return argv


def _shell_script_arg(args):
    """The script text given to a shell with -c, including bundled (-ec, -lc) and glued (-c"cmd") forms, else None."""
    for i, a in enumerate(args):
        if re.fullmatch(r"-[A-Za-z]+", a) and "c" in a[1:]:
            return args[i + 1] if i + 1 < len(args) else ""
        if a.startswith("-c") and len(a) > 2 and not re.fullmatch(r"-[A-Za-z]+", a):
            return a[2:]
    return None


def _check_segment(argv, piped, here, depth):
    original = argv
    argv = _unwrap(argv, depth)
    if not argv:
        if original and os.path.basename(original[0]) == "env":
            raise Block("dumps environment variables, which may hold credentials")
        return
    cmd, args = argv[0], argv[1:]
    if "$" in cmd or "`" in cmd or (any(ch in cmd for ch in "*?") and cmd not in ("[", "[[")):
        raise Block("the command name contains a variable, substitution or glob and cannot be inspected")
    base = os.path.basename(cmd)

    if base in OPAQUE_LAUNCHERS:
        raise Block(f"{base} launches other programs in ways this guard cannot inspect")

    if piped and base in INTERPRETERS and _shell_script_arg(args) is None and not any(a == "-c" for a in args):
        operand = next((a for a in args if not a.startswith("-") or a == "-"), None)
        reads_stdin = base in SHELLS and any(re.fullmatch(r"-[A-Za-z]*s[A-Za-z]*", a) for a in args)
        if operand is None or operand == "-" or reads_stdin:
            raise Block(f"piping into {base} with no script file runs unreviewed code from the pipe")

    if base in SHELLS:
        for text in here:
            check(text, depth + 1)
        script_arg = _shell_script_arg(args)
        if script_arg is not None:
            check(script_arg, depth + 1)
    elif base in ("pwsh", "powershell") and any(a.lower() in ("-c", "-command", "-encodedcommand") for a in args):
        raise Block("PowerShell commands cannot be inspected")
    elif base == "eval":
        check(" ".join(args), depth + 1)
    elif base == "find":
        for i, a in enumerate(args):
            if a in ("-exec", "-execdir", "-ok", "-okdir"):
                end = next((j for j in range(i + 1, len(args)) if args[j] in (";", "+", "\\;")), len(args))
                _check_segment(args[i + 1:end], False, [], depth + 1)
    elif base == "az":
        _check_az(args)
    elif base == "azd":
        _check_azd(args)
    elif base == "git":
        _check_git(args)
    elif base == "gh" and args[:2] == ["auth", "token"]:
        raise Block("prints a GitHub token")
    elif base in ("curl", "wget", "http", "https", "xh"):
        _check_http(base, args)
    elif base in ENV_DUMPERS or base == "compgen" or (base in ("env", "set", "export") and not args) or (base == "export" and args[:1] == ["-p"]):
        raise Block("dumps environment variables, which may hold credentials")

    _check_credential_paths(base, args)


def _command_path(args, value_flags, bool_flags):
    """Return the positional command words, skipping known global flags (and the values of value flags).

    Stops at the first unknown flag. Returns (words, unknown_first) where unknown_first is true when an
    unknown flag appeared before any command word, so the real command cannot be determined.
    """
    words, i = [], 0
    while i < len(args):
        a = args[i]
        if a.startswith("-"):
            name = a.split("=", 1)[0]
            if name in bool_flags:
                i += 1
                continue
            if name in value_flags:
                i += 1 if "=" in a else 2
                continue
            return words, not words
        words.append(a)
        i += 1
    return words, False


def _no_expansion(words, what):
    if any("$" in w or "`" in w for w in words):
        raise Block(f"a variable or substitution in the {what} command words cannot be inspected")


def _check_az(args):
    _no_expansion([a for a in args if not a.startswith("-")][:6], "az")
    words, unknown_first = _command_path(args, AZ_GLOBAL_VALUE_FLAGS, AZ_GLOBAL_BOOL_FLAGS)
    flags = [a.lower() for a in args if a.startswith("-")]
    if unknown_first:
        raise Block("an az option before the command word cannot be classified; put options after the command")
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
    if words[0] in AZ_READ_GROUPS and words[-1] in AZ_READ_VERBS:
        return
    raise Block(f"az {' '.join(words)} is not on the read-only allow-list (Foundry Doctor is read-only; ask the user)")


def _check_azd(args):
    _no_expansion([a for a in args if not a.startswith("-")][:6], "azd")
    words, unknown_first = _command_path(args, AZD_GLOBAL_VALUE_FLAGS, AZD_GLOBAL_BOOL_FLAGS)
    if unknown_first:
        raise Block("an azd option before the command word cannot be classified; put options after the command")
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
        if a in ("-c", "--config-env") and i + 1 < len(args):
            key = args[i + 1].split("=", 1)[0].lower()
            if key in GIT_DANGEROUS_CONFIG or key.startswith("alias."):
                raise Block(f"git -c {key} can run arbitrary programs")
            i += 2
        elif a in ("-C", "--git-dir", "--work-tree", "--namespace", "--exec-path") and i + 1 < len(args):
            i += 2
        elif a.startswith("-"):
            i += 1
        else:
            break
    if i >= len(args):
        return
    sub, rest = args[i], args[i + 1:]
    if "$" in sub or "`" in sub or (sub == "push" and any("$" in a or "`" in a for a in rest)):
        raise Block("a variable or substitution in the git subcommand or push arguments cannot be inspected")
    if sub == "config" and any(a.lower().startswith("alias.") for a in rest):
        raise Block("git config alias.* can hide a forced push")
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
    elif sub == "credential":
        raise Block("git credential prints stored credentials")
    elif sub in ("diff", "log", "show", "format-patch", "blame") and any(a.startswith("--output") or a == "--ext-diff" for a in rest):
        raise Block(f"git {sub} --output or --ext-diff writes files or runs programs")


def _check_http(base, args):
    text = " ".join(args)
    if any(a in ("-K", "--config") for a in args):
        raise Block("curl --config can hide the method and URL")
    if not HOST_RE.search(text):
        return
    mutating = False
    for i, a in enumerate(args):
        bundle = re.fullmatch(r"-[A-Za-z]+", a)
        if bundle and not a.startswith("--"):
            letters = a[1:]
            if "X" in letters:
                pre, post = letters.split("X", 1)
                method = post or (args[i + 1] if i + 1 < len(args) else "")
                if method.upper() not in ("GET", "HEAD"):
                    mutating = True
            else:
                pre = letters
            if any(ch in pre for ch in "dFT"):
                mutating = True
        low = a.lower()
        if low in ("-x", "--request", "--method") and i + 1 < len(args) and args[i + 1].upper() not in ("GET", "HEAD"):
            mutating = True
        if low.startswith("-x") and len(a) > 2 and not low.startswith("--") and a[2:].upper() not in ("GET", "HEAD"):
            mutating = True  # joined short form such as -XDELETE
        if low.startswith(("--request=", "--method=")) and low.split("=", 1)[1].upper() not in ("GET", "HEAD"):
            mutating = True
        if low in ("-d", "--data", "--data-raw", "--data-binary", "--data-urlencode", "-f", "--form", "-t", "--upload-file", "--json", "--post-data", "--post-file", "--body-data"):
            mutating = True
        if low.startswith(("--data", "--form", "--json=", "--post-")):
            mutating = True
    if mutating:
        raise Block("a non-GET request to an Azure or Microsoft endpoint")


def _credential_candidates(arg):
    """The argument as written, with ~ and $VARS expanded, plus any glob matches."""
    path = arg.split("=", 1)[1] if arg.startswith("-") and "=" in arg else arg
    if "=@" in path:                       # curl -F name=@file
        path = path.split("=@", 1)[1]
    path = path.lstrip("@<")                # curl -d @file, -F 'name=<file'
    out = {path, os.path.expanduser(os.path.expandvars(path))}
    for p in list(out):
        if any(ch in p for ch in "*?["):
            out.update(glob.glob(p)[:200])
    return out


def _check_credential_paths(base, args):
    if base in PATH_ONLY_CMDS or (base == "git" and args[:1] == ["check-ignore"]):
        return
    dirs = [os.path.expanduser(d) for d in CRED_DIRS]
    for a in args:
        if SECRET_VAR_RE.search(a):
            raise Block("prints a credential held in an environment variable")
        if a.startswith("-") and "=" not in a:
            continue
        for cand in _credential_candidates(a):
            if not cand or cand.startswith(("http://", "https://")):
                continue
            name = os.path.basename(cand.rstrip("/"))
            if name in CRED_OK_BASENAMES:
                continue
            if CRED_BASENAME_RE.match(name):
                raise Block(f"reads or copies a credential file ({name})")
            absolute = os.path.abspath(cand)
            if any(absolute == d or absolute.startswith(d + "/") for d in dirs):
                raise Block("reads a credential directory (Azure, AWS, GitHub, kube, ssh, docker or gcloud profile)")
            if re.match(r"^/proc/[^/]+/environ$", absolute):
                raise Block("reads process environment variables")
    # A glob aimed at credential dot-files is blocked even when nothing matches yet.
    for a in args:
        if re.search(r"(^|/)\.(env|az|aws|ssh|netrc|git-credentials)[^/ ]*[*?\[]", a) or re.search(r"(^|/)\.e[*?\[]", a):
            raise Block("a glob that can match credential files")


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
    except Exception as e:  # any bug in the guard must not allow the command
        print(f"Blocked by guard_bash.py: internal error while inspecting the command ({type(e).__name__}); failing closed.", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
