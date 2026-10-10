"""Regression tests for secret signatures and the tightly scoped allowlist."""
import pathlib
import shutil
import subprocess
import tempfile
import unittest

HERE = pathlib.Path(__file__).resolve().parent
BASH = shutil.which("bash")


def run_bash(command, *, input_text=None, args=(), cwd=HERE):
    """Run the installed Bash and close all pipes before temporary-file cleanup."""
    if BASH is None:
        raise RuntimeError("Bash (Git Bash on Windows) is required for secret-scanner tests")
    with subprocess.Popen(
        [BASH, "-c", command, "test-secret-scanner", *args],
        cwd=cwd,
        stdin=subprocess.PIPE if input_text is not None else subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    ) as process:
        stdout, stderr = process.communicate(input_text)
        return subprocess.CompletedProcess(process.args, process.returncode, stdout, stderr)


class SecretRegexTests(unittest.TestCase):
    def matches(self, value):
        # Pass the candidate on stdin. In particular, do not put angle-bracket
        # placeholders in the environment, where MSYS environment conversion
        # can alter them before grep sees them.
        command = 'source "$1"; grep -Eiq -- "$FD_SECRET_REGEX"'
        run = run_bash(command, input_text=value + "\n", args=((HERE / "lib.sh").as_posix(),))
        self.assertIn(run.returncode, (0, 1), run.stderr)
        return run.returncode == 0

    def test_high_signal_secret_shapes_match(self):
        samples = [
            "client_" + "sec" + "ret=abcdefghijklmnop",
            "AK" + "IA" + "ABCDEFGHIJKLMNOP",
            "AS" + "IA" + "ABCDEFGHIJKLMNOP",
            "gh" + "p_" + "a" * 36,
            "github_" + "pat_" + "a" * 40,
            "xo" + "xb-" + "a" * 20,
            "sk-" + "live-" + "a" * 24,
            "-----BEGIN " + "PRIVATE KEY-----",
            "https://x.invalid/?sig=" + "a" * 32,
        ]
        for sample in samples:
            with self.subTest(sample=sample[:20]):
                self.assertTrue(self.matches(sample))

    def test_placeholders_do_not_match(self):
        for sample in ["client_" + "secret=<redacted>", "api_" + "key=example",
                       "github_" + "pat_EXAMPLE"]:
            with self.subTest(sample=sample):
                self.assertFalse(self.matches(sample))


class AllowlistTests(unittest.TestCase):
    def check(self, entry):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp, "foundry-doctor", "scripts", "claude")
            root.mkdir(parents=True)
            (root / "lib.sh").write_text((HERE / "lib.sh").read_text(), encoding="utf-8")
            (root / "secret-allowlist.txt").write_text(entry + "\n", encoding="utf-8")
            fixture = pathlib.Path(tmp, "foundry-doctor", entry)
            if ".." not in entry and not entry.startswith("/") and "*" not in entry:
                fixture.parent.mkdir(parents=True, exist_ok=True)
                fixture.write_text("fake", encoding="utf-8")
            # Do not make the MSYS process's cwd a directory that Windows must
            # delete immediately afterwards. Source by absolute POSIX-style path.
            run = run_bash(
                'source "$1"; fd_secret_excludes',
                args=((root / "lib.sh").as_posix(),),
                cwd=HERE,
            )
            return run

    def test_fixture_file_is_allowed(self):
        run = self.check("test/fixtures/redaction/fake.env")
        self.assertEqual(run.returncode, 0, run.stderr)

    def test_broad_or_unsafe_entries_are_rejected(self):
        for entry in ["**", "scripts/file.txt", "../file.txt", "test/file.txt",
                      "/tmp/file.txt", "test/fixtures/**"]:
            with self.subTest(entry=entry):
                self.assertNotEqual(self.check(entry).returncode, 0)


if __name__ == "__main__":
    unittest.main()
