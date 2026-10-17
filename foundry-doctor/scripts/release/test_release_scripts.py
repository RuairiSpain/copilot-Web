import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import check_namespace_collision as cnc  # noqa: E402
import gen_extension_manifest as gen  # noqa: E402

H = "a" * 64

# Minimal vendored subset of azd registry.schema.json (Azure/azure-dev,
# cli/azd/extensions/registry.schema.json), checked 2026-10-17. No network in tests.
SCHEMA_EXTENSION_REQUIRED = ["id", "namespace", "displayName", "description", "versions"]
SCHEMA_VERSION_REQUIRED = ["version", "usage", "examples"]
SCHEMA_VERSION_PATTERN = r"^\d+\.\d+\.\d+$"
SCHEMA_EXAMPLE_REQUIRED = ["name", "description", "usage"]
SCHEMA_ARTIFACT_REQUIRED = ["url"]
SCHEMA_CHECKSUM_REQUIRED = ["algorithm", "value"]
SCHEMA_ARTIFACT_PROPS = {"checksum", "entryPoint", "url"}


class GenTests(unittest.TestCase):
    def sums(self, v="1.2.3"):
        return {
            f"foundry-doctor-azd-extension_{v}_windows_{a}.zip": H
            for a in ("amd64", "arm64")
        } | {f"foundry-doctor_{v}_windows_amd64.zip": H}

    def test_build_lists_only_extension_artifacts(self):
        m, r = gen.build("1.2.3", self.sums(), "https://x/y")
        arts = r["extensions"][0]["versions"][0]["artifacts"]
        self.assertIsInstance(arts, dict)
        self.assertEqual(sorted(arts), ["windows/amd64", "windows/arm64"])
        self.assertEqual(len(m["platforms"]), 2)
        self.assertTrue(all(a["checksum"]["algorithm"] == "sha256" for a in arts.values()))
        self.assertEqual(arts["windows/arm64"]["entryPoint"], "foundry-doctor-azd.exe")
        self.assertTrue(arts["windows/arm64"]["url"].endswith("_windows_arm64.zip"))

    def test_registry_matches_schema_structure(self):
        import re

        _, r = gen.build("1.2.3", self.sums(), "https://x/y")
        for ext in r["extensions"]:
            for k in SCHEMA_EXTENSION_REQUIRED:
                self.assertIn(k, ext)
            for v in ext["versions"]:
                for k in SCHEMA_VERSION_REQUIRED:
                    self.assertIn(k, v)
                self.assertRegex(v["version"], SCHEMA_VERSION_PATTERN)
                self.assertGreaterEqual(len(v["examples"]), 1)
                for e in v["examples"]:
                    for k in SCHEMA_EXAMPLE_REQUIRED:
                        self.assertIn(k, e)
                self.assertIsInstance(v["artifacts"], dict)
                self.assertGreaterEqual(len(v["artifacts"]), 1)
                for key, art in v["artifacts"].items():
                    self.assertRegex(key, r"^[a-z0-9]+(/[a-z0-9]+)?$")
                    self.assertNotIn("platform", art)
                    self.assertLessEqual(set(art), SCHEMA_ARTIFACT_PROPS)
                    for k in SCHEMA_ARTIFACT_REQUIRED:
                        self.assertIn(k, art)
                    self.assertTrue(re.match(r"^https?://", art["url"]))
                    for k in SCHEMA_CHECKSUM_REQUIRED:
                        self.assertIn(k, art["checksum"])

    def test_linux_and_tar_gz_artifacts_ignored(self):
        sums = self.sums() | {
            f"foundry-doctor-azd-extension_1.2.3_linux_amd64.tar.gz": H,
            f"foundry-doctor-azd-extension_1.2.3_darwin_arm64.tar.gz": H,
            f"foundry-doctor-azd-extension_1.2.3_windows_amd64.tar.gz": H,
        }
        _, r = gen.build("1.2.3", sums, "https://x/y")
        self.assertEqual(
            sorted(r["extensions"][0]["versions"][0]["artifacts"]), ["windows/amd64", "windows/arm64"]
        )
        only_linux = {"foundry-doctor-azd-extension_1.2.3_linux_amd64.tar.gz": H}
        with self.assertRaises(ValueError):
            gen.build("1.2.3", only_linux, "https://x/y")

    def test_prerelease_rejected_with_clear_error(self):
        with self.assertRaises(ValueError) as cm:
            gen.build("1.2.3-rc.1", self.sums("1.2.3-rc.1"), "https://x/y")
        self.assertIn("prerelease", str(cm.exception))

    def test_rejects_bad_semver_and_forbidden_id(self):
        with self.assertRaises(ValueError):
            gen.build("v1", self.sums(), "https://x")
        with self.assertRaises(ValueError):
            gen.build("1.2.3", self.sums(), "https://x", "microsoft.foundry")

    def test_no_artifacts_is_error(self):
        with self.assertRaises(ValueError):
            gen.build("1.2.3", {"other.zip": H}, "https://x")

    def test_parse_checksums(self):
        self.assertEqual(gen.parse_checksums(f"{H}  dist/a.zip\n"), {"a.zip": H})
        with self.assertRaises(ValueError):
            gen.parse_checksums("garbage")


class CollisionTests(unittest.TestCase):
    def test_collision_detection(self):
        reg = {"extensions": [{"id": "a", "namespace": "foundry.x"}, {"id": "b", "namespace": "ai"}]}
        self.assertEqual(len(cnc.find_collisions(reg, "foundry")), 1)
        self.assertEqual(cnc.find_collisions({"extensions": [{"id": "c", "namespace": "foundryx"}]}, "foundry"), [])

    def test_main_local_files(self):
        import tempfile

        with tempfile.TemporaryDirectory() as d:
            f = Path(d) / "r.json"
            f.write_text(json.dumps({"extensions": [{"id": "z", "namespace": "foundry"}]}))
            self.assertEqual(cnc.main(["--registry-file", str(f)]), 1)
            self.assertEqual(cnc.main(["--registry-file", str(Path(d) / "missing.json")]), 2)


if __name__ == "__main__":
    unittest.main()
