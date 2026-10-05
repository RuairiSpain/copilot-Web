import pathlib
import tempfile
import unittest
from unittest import mock

import dependency_audit


class LicenceTests(unittest.TestCase):
    def licence(self, text):
        with tempfile.TemporaryDirectory() as directory:
            pathlib.Path(directory, "LICENSE").write_text(text, encoding="utf-8")
            return dependency_audit.licence_for(directory)

    def test_recognises_mit(self):
        self.assertEqual(self.licence(
            "Permission is hereby granted, free of charge, to any person"), "MIT")

    def test_recognises_dual_mit_apache(self):
        text = ("Permission is hereby granted, free of charge\n"
                "Apache License\nVersion 2.0, January 2004")
        self.assertEqual(self.licence(text), "MIT AND Apache-2.0")

    def test_spdx_expressions_are_preferred_and_validated(self):
        self.assertEqual(
            self.licence("SPDX-License-Identifier: MIT OR Apache-2.0"),
            "MIT OR Apache-2.0",
        )
        for expression in (
                "MIT AND", "MIT WITH LLVM-exception", "LicenseRef-Custom",
                "GPL-2.0-only WITH Classpath-exception-2.0"):
            with self.subTest(expression=expression), self.assertRaises(RuntimeError):
                self.licence(f"SPDX-License-Identifier: {expression}")

    def test_malformed_spdx_metadata_fails_even_with_mit_text(self):
        malformed = (
            "SPDX-License-Identifier MIT\n"
            "Permission is hereby granted, free of charge"
        )
        ambiguous = (
            "SPDX-License-Identifier: MIT\n"
            "SPDX-License-Identifier: Apache-2.0"
        )
        for text in (malformed, ambiguous):
            with self.subTest(text=text), self.assertRaises(RuntimeError):
                self.licence(text)

    def test_prohibited_text_cannot_hide_in_an_approved_licence(self):
        for prohibited in (
                "GNU General Public License version 3",
                "GNU Lesser General Public License",
                "GNU Affero General Public License",
                "SPDX-License-Identifier: LGPL-2.1-only"):
            with self.subTest(prohibited=prohibited), self.assertRaises(RuntimeError):
                self.licence(
                    "Permission is hereby granted, free of charge\n" + prohibited
                )

    def test_all_licence_files_are_classified_and_notice_is_supplemental(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "LICENSE-MIT").write_text(
                "Permission is hereby granted, free of charge", encoding="utf-8")
            (root / "COPYING.apache").write_text(
                "Apache License\nVersion 2.0, January 2004", encoding="utf-8")
            (root / "NOTICE").write_text("Copyright Example", encoding="utf-8")
            self.assertEqual(
                dependency_audit.licence_for(root), "Apache-2.0 AND MIT")
            (root / "LICENSE-custom").write_text("custom terms", encoding="utf-8")
            with self.assertRaises(RuntimeError):
                dependency_audit.licence_for(root)

    def test_recognises_bsd_and_isc(self):
        bsd = ("Redistribution and use in source and binary forms are permitted. "
               "THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS")
        self.assertEqual(self.licence(bsd), "BSD-2-Clause")
        isc = ('Permission to use, copy, modify, and/or distribute this software. '
               'THE SOFTWARE IS PROVIDED "AS IS"')
        self.assertEqual(self.licence(isc), "ISC")

    def test_unknown_and_missing_licences_fail_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(RuntimeError):
                dependency_audit.licence_for(directory)
        with self.assertRaises(RuntimeError):
            self.licence("custom terms")

    @mock.patch.object(dependency_audit, "module_graph")
    def test_malformed_module_metadata_fails_closed(self, graph):
        for metadata in ({}, {"Path": "example/a"}, {"Error": {"Err": "bad"}}):
            with self.subTest(metadata=metadata):
                graph.return_value = [metadata]
                with self.assertRaises(RuntimeError):
                    dependency_audit.audit()

    @mock.patch.object(dependency_audit, "go_json")
    def test_download_metadata_must_match_and_be_checksum_verified(self, go_json):
        valid = {
            "Path": "example/a", "Version": "v1.0.0", "Sum": "h1:value",
            "Dir": "/cache/example",
        }
        for metadata in (
                {**valid, "Path": "example/other"},
                {**valid, "Version": "v2.0.0"},
                {key: value for key, value in valid.items() if key != "Sum"},
                {**valid, "Error": "download failed"}):
            with self.subTest(metadata=metadata):
                go_json.return_value = [metadata]
                with self.assertRaises(RuntimeError):
                    dependency_audit.module_directory("example/a", "v1.0.0")

    def test_tool_pin_checker_validates_all_three_sources(self):
        workflow = (
            "GOVULNCHECK_VERSION: v1.7.0\n"
            "STATICCHECK_VERSION: v0.6.1\n"
            "ref: afe4b2b4d262bab4c11f4937e7a7942557ab0ecd\n"
            '"golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"\n'
            '"honnef.co/go/tools/cmd/staticcheck@${STATICCHECK_VERSION}"\n')
        installer = (
            "go install golang.org/x/vuln/cmd/govulncheck@v1.7.0\n"
            "go install honnef.co/go/tools/cmd/staticcheck@v0.6.1\n")
        inventory = (
            "`afe4b2b4d262bab4c11f4937e7a7942557ab0ecd`\n"
            "`golang.org/x/vuln/cmd/govulncheck@v1.7.0`\n"
            "`honnef.co/go/tools/cmd/staticcheck@v0.6.1`\n")
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            paths = [root / name for name in ("workflow", "install", "inventory")]
            for path, text in zip(paths, (workflow, installer, inventory)):
                path.write_text(text, encoding="utf-8")
            with (mock.patch.object(dependency_audit, "WORKFLOW", paths[0]),
                  mock.patch.object(dependency_audit, "INSTALL_TOOLS", paths[1]),
                  mock.patch.object(dependency_audit, "INVENTORY", paths[2])):
                dependency_audit.check_tool_pins()
                paths[1].write_text(installer.replace("v1.7.0", "v1.8.0"),
                                    encoding="utf-8")
                with self.assertRaises(RuntimeError):
                    dependency_audit.check_tool_pins()

    def test_notice_renderer_uses_selected_checksum_verified_material(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "LICENSE").write_text(
                "Permission is hereby granted, free of charge", encoding="utf-8")
            row = [("example/a", "v1.0.0", "MIT")]
            with mock.patch.object(
                    dependency_audit, "module_directory", return_value=directory):
                rendered = dependency_audit.render_notices(row)
        self.assertIn("## example/a v1.0.0", rendered)
        self.assertIn("Licence classification: `MIT`", rendered)
        self.assertIn("### LICENSE", rendered)

    @mock.patch.object(dependency_audit, "inventory_rows")
    @mock.patch.object(dependency_audit, "audit")
    def test_inventory_mismatch_fails(self, audit, inventory):
        audit.return_value = [("example/a", "v1.0.0", "MIT")]
        inventory.return_value = [("example/a", "v1.0.1", "MIT")]
        self.assertEqual(dependency_audit.main(["--check-inventory"]), 1)


if __name__ == "__main__":
    unittest.main()
