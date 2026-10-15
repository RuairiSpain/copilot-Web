import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import check_namespace_collision as cnc  # noqa: E402
import gen_extension_manifest as gen  # noqa: E402

H = "a" * 64


class GenTests(unittest.TestCase):
    def sums(self, v="1.2.3"):
        return {
            f"foundry-doctor-azd-extension_{v}_{o}_{a}.{'zip' if o == 'windows' else 'tar.gz'}": H
            for o in ("linux", "darwin", "windows")
            for a in ("amd64", "arm64")
        } | {f"foundry-doctor_{v}_linux_amd64.tar.gz": H}

    def test_build_lists_only_extension_artifacts(self):
        m, r = gen.build("1.2.3", self.sums(), "https://x/y")
        arts = r["extensions"][0]["versions"][0]["artifacts"]
        self.assertEqual(len(arts), 6)
        self.assertEqual(len(m["platforms"]), 6)
        self.assertTrue(all(a["checksum"]["algorithm"] == "sha256" for a in arts))

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
