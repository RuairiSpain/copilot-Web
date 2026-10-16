package ops

import "testing"

func TestScanShellForAzd(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   shellResult
	}{
		{"plain up", "azd up", shellFound},
		{"provision with flags", "azd provision --no-prompt", shellFound},
		{"deploy after login", "azd auth login\nazd deploy", shellFound},
		{"after and-and", "echo hi && azd up", shellFound},
		{"after semicolon", "echo hi; azd deploy", shellFound},
		{"after or-or", "false || azd up", shellFound},
		{"after pipe", "echo y | azd up", shellFound},
		{"command substitution", "x=$(azd provision)", shellFound},
		{"backticks", "x=`azd deploy`", shellFound},
		{"env assignment prefix", "AZURE_ENV_NAME=prod azd up", shellFound},
		{"sudo prefix", "sudo azd up", shellFound},
		{"azd.exe", "azd.exe up", shellFound},
		{"uppercase subcommand", "azd UP", shellFound},
		{"inside if", "if [ -n \"$X\" ]; then azd up; fi", shellFound},
		{"else of dead if is live", "if false; then echo a; else azd up; fi", shellFound},
		{"line continuation", "azd \\\n  up", shellFound},
		{"crlf", "echo hi\r\nazd up\r\n", shellFound},
		{"exit guard with or does not hide later azd", "cmd || exit 1\nazd up", shellFound},
		{"live code after dead if", "if false; then echo x; fi\nazd up", shellFound},
		{"case branch", "case $X in a) azd up;; esac", shellFound},

		{"echo azd up", "echo azd up", shellNone},
		{"echo provision", "echo azd provision", shellNone},
		{"trailing comment", "echo hi # azd up", shellNone},
		{"comment line", "# azd up\necho hi", shellNone},
		{"quoted string", `echo "skipping azd up"`, shellNone},
		{"single quoted", `echo 'azd deploy'`, shellNone},
		{"quoted command word", `"azd up"`, shellNone},
		{"dead if false", "if false; then azd up; fi", shellNone},
		{"dead if not true", "if ! true; then azd deploy; fi", shellNone},
		{"dead while false", "while false; do azd up; done", shellNone},
		{"dead if then echo", "if false; then azd up; fi\necho done", shellNone},
		{"azd auth login", "azd auth login", shellNone},
		{"azd alone", "azd", shellNone},
		{"other tool arg", "terraform apply azd up", shellNone},
		{"path argument", "cd /opt/azd up", shellNone},
		{"empty", "", shellNone},

		{"heredoc is uncertain", "cat <<EOF\nazd up\nEOF", shellUncertain},
		{"function definition", "deploy() { azd up; }\n", shellUncertain},
		{"function definition with space", "deploy () { azd up; }\n", shellUncertain},
		{"function keyword", "function deploy { azd up; }", shellUncertain},
		{"unterminated quote", `echo "azd up`, shellUncertain},
		{"sh -c", `sh -c "azd up"`, shellUncertain},
		{"eval", `eval "azd up"`, shellUncertain},
		{"substitution in string", `echo "$(azd up)"`, shellUncertain},
		{"computed subcommand", "azd $CMD", shellUncertain},
		{"global flag before subcommand", "azd --cwd app up", shellUncertain},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := scanShellForAzd(tc.script); got != tc.want {
				t.Errorf("scanShellForAzd(%q) = %d, want %d", tc.script, got, tc.want)
			}
		})
	}
}
