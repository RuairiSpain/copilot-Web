"""Unit tests for the parts of the demos that don't need Azure.

The Azure calls themselves are not exercised here — there is no subscription in
CI — so `azure.identity` is stubbed at import time and the tests cover the
logic that is genuinely ours: corpus chunking and classification, the trace
renderer, the toolbox YAML emitter, tool selection and the inventory's
degradation and Markdown rendering.

    python -m unittest discover -s demos/tests -v
"""

from __future__ import annotations

import os
import sys
import types
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPO / "demos" / "foundry-iq" / "scripts"))
sys.path.insert(0, str(REPO / "demos" / "foundry-toolbox" / "scripts"))

# Stub azure.identity: importing the real one needs a working cffi build, and
# none of these tests authenticate.
if "azure.identity" not in sys.modules:
    azure = types.ModuleType("azure")
    identity = types.ModuleType("azure.identity")

    class _Credential:  # pragma: no cover - never called
        def get_token(self, *_args, **_kwargs):
            raise AssertionError("tests must not authenticate")

    identity.DefaultAzureCredential = _Credential
    azure.identity = identity
    sys.modules.setdefault("azure", azure)
    sys.modules["azure.identity"] = identity

import inventory  # noqa: E402
import toolbox_spec  # noqa: E402
import trace_view  # noqa: E402
from index_schema import FILTERABLE_METADATA, SEMANTIC_CONFIG, VECTOR_PROFILE, build_index  # noqa: E402

sys.modules.pop("create_toolbox", None)
import create_toolbox  # noqa: E402


class TestIndexSchema(unittest.TestCase):
    def setUp(self) -> None:
        self.index = build_index(
            "hr-templates-index",
            aoai_endpoint="https://example.openai.azure.com",
            embedding_deployment="text-embedding-3-large",
            embedding_model="text-embedding-3-large",
            dimensions=3072,
        )
        self.fields = {f["name"]: f for f in self.index["fields"]}

    def test_hybrid_retrieval_is_configured(self) -> None:
        # BM25 side.
        self.assertTrue(self.fields["content"]["searchable"])
        # Vector side, wired to the profile that carries the vectorizer.
        self.assertEqual(self.fields["content_vector"]["vectorSearchProfile"], VECTOR_PROFILE)
        self.assertEqual(self.fields["content_vector"]["dimensions"], 3072)
        profile = self.index["vectorSearch"]["profiles"][0]
        self.assertEqual(profile["name"], VECTOR_PROFILE)
        self.assertEqual(profile["vectorizer"], self.index["vectorSearch"]["vectorizers"][0]["name"])

    def test_every_declared_metadata_field_is_filterable(self) -> None:
        # The knowledge source's queryHints promise the planner these fields
        # can be filtered on; if one stopped being filterable the filter would
        # fail at query time, not at deploy time.
        for name in FILTERABLE_METADATA:
            with self.subTest(field=name):
                self.assertTrue(self.fields[name].get("filterable"), f"{name} must be filterable")

    def test_semantic_configuration_matches_the_knowledge_source(self) -> None:
        names = [c["name"] for c in self.index["semantic"]["configurations"]]
        self.assertIn(SEMANTIC_CONFIG, names)

    def test_vector_field_is_not_returned_to_callers(self) -> None:
        # 3072 floats per chunk in every result is pure payload weight.
        self.assertFalse(self.fields["content_vector"]["retrievable"])


class TestCorpusProcessing(unittest.TestCase):
    def setUp(self) -> None:
        import importlib

        self.provision = importlib.import_module("2_provision") if "2_provision" in sys.modules else None
        if self.provision is None:
            spec = importlib.util.spec_from_file_location(
                "provision_mod", REPO / "demos" / "foundry-iq" / "scripts" / "2_provision.py"
            )
            self.provision = importlib.util.module_from_spec(spec)
            assert spec.loader is not None
            sys.modules["provision_mod"] = self.provision
            spec.loader.exec_module(self.provision)

    def test_chunks_overlap_and_cover_the_text(self) -> None:
        text = "\n\n".join(f"Paragraph {i} " + "word " * 80 for i in range(20))
        chunks = list(self.provision.chunk(text))
        self.assertGreater(len(chunks), 1)
        self.assertTrue(all(c.strip() for c in chunks))
        # First and last content must both survive chunking.
        self.assertIn("Paragraph 0", chunks[0])
        self.assertIn("Paragraph 19", chunks[-1])

    def test_chunking_terminates_on_a_single_long_unbroken_run(self) -> None:
        # A boundary-free string is the case where a naive overlap loop spins.
        chunks = list(self.provision.chunk("x" * 9000))
        self.assertGreater(len(chunks), 1)
        self.assertLess(len(chunks), 20)

    def test_empty_text_yields_no_chunks(self) -> None:
        self.assertEqual(list(self.provision.chunk("   \n\n  ")), [])

    def test_classification_picks_type_and_tags(self) -> None:
        doc_type, tags = self.provision.classify(
            "This grievance procedure explains dismissal and notice period rules.", fallback="guidance"
        )
        self.assertEqual(doc_type, "procedure")
        self.assertIn("dismissal", tags)

    def test_classification_falls_back_when_nothing_matches(self) -> None:
        doc_type, tags = self.provision.classify("Nothing relevant here at all.", fallback="guidance")
        self.assertEqual(doc_type, "guidance")
        self.assertEqual(tags, [])

    def test_slugify_produces_a_valid_search_document_key(self) -> None:
        # Search keys allow letters, digits, _, - and =; anything else breaks
        # the upload with a 400 that is tedious to trace back.
        key = self.provision.slugify("hr-templates/Écrit type (v2).pdf")
        self.assertRegex(key, r"^[A-Za-z0-9_\-=]+$")


class TestTraceView(unittest.TestCase):
    ACTIVITY = [
        {"type": "modelQueryPlanning", "id": 0, "elapsedMs": 420, "inputTokens": 1204, "outputTokens": 88,
         "model": {"modelName": "gpt-5.4-mini"}},
        {"type": "searchIndex", "id": 1, "elapsedMs": 310, "knowledgeSourceName": "eu-directives-ks", "count": 6,
         "searchIndexArguments": {"search": "written statement", "filter": "language eq 'en'"}},
        {"type": "searchIndex", "id": 2, "elapsedMs": 290, "knowledgeSourceName": "hr-templates-ks", "count": 4,
         "searchIndexArguments": {"search": "template", "filter": "doc_type eq 'contract'"}},
        {"type": "modelAnswerSynthesis", "id": 3, "elapsedMs": 980, "inputTokens": 8102, "outputTokens": 412,
         "model": {"modelName": "gpt-5.4-mini"}},
    ]

    def test_totals_add_up(self) -> None:
        counts = trace_view.totals(self.ACTIVITY)
        self.assertEqual(counts["subqueries"], 2)
        self.assertEqual(counts["docs"], 10)
        self.assertEqual(counts["in"], 9306)
        self.assertEqual(counts["out"], 500)

    def test_filters_are_surfaced(self) -> None:
        # The filter line is the point of the trace: it shows metadata
        # narrowing the candidate set before vectors are compared.
        rendered = "\n".join(line for r in self.ACTIVITY for line in trace_view.format_record(r))
        self.assertIn("doc_type eq 'contract'", rendered)
        self.assertIn("eu-directives-ks", rendered)

    def test_summary_mentions_wall_time(self) -> None:
        self.assertIn("2,041 ms wall", trace_view.summarize(self.ACTIVITY, 2041))

    def test_unknown_activity_type_does_not_crash(self) -> None:
        lines = trace_view.format_record({"type": "somethingNew", "elapsedMs": 10})
        self.assertEqual(len(lines), 1)


class TestToolboxSpec(unittest.TestCase):
    """Guards the tool declarations against the real SDK contract.

    Every check here exists because the natural-reading name was wrong:
    `agent_to_agent`, `browser_automation`, `fabric_iq`, `work_iq` and a
    `skills` tool type were all plausible and all invalid. These assertions are
    what stops that class of mistake coming back.
    """

    def setUp(self) -> None:
        os.environ.setdefault("AI_SEARCH_CONNECTION_ID", "conn-test")
        os.environ.setdefault("AI_SEARCH_INDEX", "idx-test")

    def test_every_catalogue_type_is_a_real_ToolboxToolType(self) -> None:
        for entry in toolbox_spec.CATALOGUE:
            with self.subTest(tool=entry["id"]):
                self.assertIn(entry["payload"]["type"], toolbox_spec.TOOLBOX_TOOL_TYPES)

    def test_the_names_that_read_naturally_are_the_wrong_ones(self) -> None:
        for invented in ("agent_to_agent", "browser_automation", "fabric_iq", "work_iq", "skills"):
            with self.subTest(invented=invented):
                self.assertNotIn(invented, toolbox_spec.TOOLBOX_TOOL_TYPES)
        for real in ("a2a", "browser_automation_preview", "fabric_iq_preview", "work_iq_preview"):
            with self.subTest(real=real):
                self.assertIn(real, toolbox_spec.TOOLBOX_TOOL_TYPES)

    def test_skills_is_a_create_version_parameter_not_a_tool(self) -> None:
        self.assertNotIn("skills", {e["payload"]["type"] for e in toolbox_spec.CATALOGUE})
        body = create_toolbox.body_for("d", [{"type": "web_search"}], ["my-skill"])
        self.assertEqual(body["skills"], ["my-skill"])
        # Absent rather than empty when no skills are requested.
        self.assertNotIn("skills", create_toolbox.body_for("d", [{"type": "web_search"}], []))

    def test_mcp_toolbox_tools_carry_no_agent_level_fields(self) -> None:
        # require_approval and allowed_tools exist on MCPTool, not MCPToolboxTool.
        mcp = next(e["payload"] for e in toolbox_spec.CATALOGUE if e["id"] == "mcp")
        kb = toolbox_spec.knowledge_base_tool("kb", "https://s/kb/mcp")
        for payload in (mcp, kb):
            self.assertNotIn("require_approval", payload)
            self.assertNotIn("allowed_tools", payload)

    def test_azure_ai_search_payload_is_nested_under_its_own_key(self) -> None:
        tools, _ = create_toolbox.select_tools(only=["azure_ai_search"], skip=[], require_all=False)
        self.assertEqual(len(tools), 1)
        index = tools[0]["azure_ai_search"]["indexes"][0]
        self.assertEqual(index["index_name"], "idx-test")
        self.assertEqual(index["query_type"], "vector_semantic_hybrid")
        # A flat index_name would be silently ignored by the service.
        self.assertNotIn("index_name", tools[0])

    def test_validate_rejects_an_invented_type(self) -> None:
        with self.assertRaises(SystemExit):
            create_toolbox.validate([{"type": "agent_to_agent"}])
        create_toolbox.validate([{"type": "a2a"}])  # must not raise

    def test_preview_tools_are_detected_so_allow_preview_gets_set(self) -> None:
        self.assertTrue(create_toolbox.uses_preview([{"type": "fabric_iq_preview"}]))
        self.assertFalse(create_toolbox.uses_preview([{"type": "web_search"}]))

    def test_connection_ids_are_found_at_any_depth(self) -> None:
        tools, _ = create_toolbox.select_tools(only=["azure_ai_search"], skip=[], require_all=False)
        self.assertEqual(create_toolbox.connection_ids(tools), ["conn-test"])

    def test_tool_search_is_included_and_flagged(self) -> None:
        tools, _ = create_toolbox.select_tools(only=[], skip=[], require_all=False)
        self.assertIn("toolbox_search", [t["type"] for t in tools])

    def test_tools_missing_prerequisites_are_skipped_not_fatal(self) -> None:
        tools, notes = create_toolbox.select_tools(only=["mcp", "web_search"], skip=[], require_all=False)
        self.assertEqual([t["type"] for t in tools], ["web_search"])
        self.assertTrue(any("needs MCP_SERVER_URL" in n for n in notes))

    def test_require_all_makes_missing_prerequisites_fatal(self) -> None:
        with self.assertRaises(SystemExit):
            create_toolbox.select_tools(only=["mcp"], skip=[], require_all=True)

    def test_unresolved_placeholders_are_pruned_recursively(self) -> None:
        pruned = create_toolbox.prune_empty(
            create_toolbox.resolve({"a": "${NOT_SET_ANYWHERE}", "b": "kept", "n": {"c": "${ALSO_NOT_SET}", "d": 1}})
        )
        self.assertEqual(pruned, {"b": "kept", "n": {"d": 1}})

    def test_yaml_round_trips_through_a_real_parser(self) -> None:
        try:
            import yaml  # type: ignore[import-not-found]
        except ImportError:
            self.skipTest("PyYAML not installed")
        tools, _ = create_toolbox.select_tools(
            only=["toolbox_search", "web_search", "azure_ai_search"], skip=[], require_all=False
        )
        parsed = yaml.safe_load(create_toolbox.to_yaml("A toolbox: with a colon", tools, ["conn-test"], ["sk"]))
        self.assertEqual(parsed["description"], "A toolbox: with a colon")
        self.assertEqual(parsed["connections"], [{"name": "conn-test"}])
        self.assertEqual(parsed["skills"], ["sk"])
        self.assertEqual([t["type"] for t in parsed["tools"]], ["toolbox_search", "web_search", "azure_ai_search"])
        # The nested block must survive the hand-rolled emitter intact.
        self.assertEqual(parsed["tools"][2]["azure_ai_search"]["indexes"][0]["top_k"], 5)

    def test_knowledge_base_tool_is_a_valid_mcp_entry(self) -> None:
        tool = toolbox_spec.knowledge_base_tool("es-employment-kb", "https://s.search.windows.net/kb/mcp")
        self.assertEqual(tool["type"], "mcp")
        self.assertIn(tool["type"], toolbox_spec.TOOLBOX_TOOL_TYPES)
        self.assertIn("knowledge_base_retrieve", tool["server_description"])


class TestRestContract(unittest.TestCase):
    """Pins the request shapes to the 2026-08-01-preview search spec.

    Each assertion here corresponds to a defect found by reading search.json:
    the data plane is OData-addressed, the output-mode enum is `extractiveData`
    (not `extractedData`), `KnowledgeSourceParams` has a required `kind`
    discriminator, and `SearchIndexKnowledgeSourceQueryHints` uses
    filters/field/fieldValues rather than filterHints/fieldName/description.
    """

    def setUp(self) -> None:
        import importlib.util

        def load(name: str, path: str):
            if name in sys.modules:
                return sys.modules[name]
            spec = importlib.util.spec_from_file_location(name, REPO / "demos" / "foundry-iq" / "scripts" / path)
            module = importlib.util.module_from_spec(spec)
            assert spec.loader is not None
            # Register before exec: @dataclass resolves sys.modules[cls.__module__],
            # which is None for a module that was never registered.
            sys.modules[name] = module
            spec.loader.exec_module(module)
            return module

        self.common = load("iq_common", "_common.py")
        self.search = load("iq_search", "3_search.py")

    def test_odata_quoting_matches_the_spec_path_shape(self) -> None:
        # Paths are /collection('name'), not /collection/name.
        self.assertEqual(self.common.odata("es-employment-kb"), "('es-employment-kb')")
        # A quote in a name is escaped by doubling, per OData literal rules.
        self.assertEqual(self.common.odata("o'brien"), "('o''brien')")

    def test_output_mode_uses_the_spec_enum_value(self) -> None:
        request = self.search.build_request(
            [("user", "q")], effort="auto", sources=[], source_kinds={},
            source_filter=None, output_mode="extractiveData", max_documents=8,
        )
        self.assertEqual(request["outputMode"], "extractiveData")

    def test_knowledge_source_params_carry_the_required_kind_discriminator(self) -> None:
        request = self.search.build_request(
            [("user", "q")], effort="low",
            sources=["hr-templates-ks", "eu-directives-ks"],
            source_kinds={"eu-directives-ks": "azureBlob", "hr-templates-ks": "searchIndex"},
            source_filter="doc_type eq 'letter'", output_mode="answerSynthesis", max_documents=4,
        )
        params = {p["knowledgeSourceName"]: p for p in request["knowledgeSourceParams"]}
        self.assertEqual(params["eu-directives-ks"]["kind"], "azureBlob")
        self.assertEqual(params["hr-templates-ks"]["kind"], "searchIndex")
        # filterAddOn exists only on the searchIndex variant.
        self.assertIn("filterAddOn", params["hr-templates-ks"])
        self.assertNotIn("filterAddOn", params["eu-directives-ks"])

    def test_include_activity_is_requested_or_the_trace_is_empty(self) -> None:
        request = self.search.build_request(
            [("user", "q")], effort="auto", sources=[], source_kinds={},
            source_filter=None, output_mode="answerSynthesis", max_documents=8,
        )
        self.assertIs(request["includeActivity"], True)


class TestTraceViewSpecShapes(unittest.TestCase):
    def test_blob_source_activity_is_rendered_as_a_source_not_an_unknown(self) -> None:
        record = {
            "type": "azureBlob", "id": 1, "elapsedMs": 120,
            "knowledgeSourceName": "eu-directives-ks", "count": 6,
            "azureBlobArguments": {"search": "working time limit"},
        }
        rendered = "\n".join(trace_view.format_record(record))
        self.assertIn("eu-directives-ks", rendered)
        self.assertIn("working time limit", rendered)
        self.assertEqual(trace_view.totals([record])["subqueries"], 1)

    def test_generated_filter_from_query_hints_is_surfaced(self) -> None:
        # The whole point of queryHints: show what the planner derived.
        record = {
            "type": "searchIndex", "id": 2, "elapsedMs": 90,
            "knowledgeSourceName": "hr-templates-ks", "count": 3,
            "searchIndexArguments": {"search": "dismissal letter", "queryType": "semantic"},
            "queryHintProcessing": {"generatedFilter": "doc_type eq 'letter'", "generatedBoost": "title"},
        }
        rendered = "\n".join(trace_view.format_record(record))
        self.assertIn("hint filter", rendered)
        self.assertIn("doc_type eq 'letter'", rendered)
        self.assertIn("hint boost", rendered)
        self.assertIn("semantic", rendered)


class TestInventory(unittest.TestCase):
    def test_values_handles_the_shapes_azure_actually_returns(self) -> None:
        self.assertEqual(inventory.values({"value": [{"a": 1}]}), [{"a": 1}])
        self.assertEqual(inventory.values({"data": [{"a": 2}]}), [{"a": 2}])
        self.assertEqual(inventory.values([{"a": 3}]), [{"a": 3}])
        self.assertEqual(inventory.values({"name": "single"}), [{"name": "single"}])

    def test_pick_falls_through_alternative_names(self) -> None:
        self.assertEqual(inventory.pick({"displayName": "x"}, "name", "displayName"), "x")
        self.assertEqual(inventory.pick({"name": ""}, "name", default="fallback"), "fallback")

    def test_markdown_reports_unavailable_sections_with_a_reason(self) -> None:
        sections = [
            inventory.Section(key="agents", title="Agents", columns=["name"], items=[{"name": "a1"}], source="GET /agents"),
            inventory.Section(key="gateways", title="AI gateways", error="LookupError: not available at /aigateways (HTTP 404)"),
        ]
        markdown = inventory.render_markdown(sections, {"project endpoint": "https://example"})
        self.assertIn("| Agents | 1 | ok |", markdown)
        self.assertIn("unavailable", markdown)
        self.assertIn("HTTP 404", markdown)
        # A probe that didn't answer must not read as "you don't have it".
        self.assertIn("does not mean the capability doesn't exist", markdown)

    def test_markdown_escapes_pipes_so_tables_survive(self) -> None:
        section = inventory.Section(key="t", title="T", columns=["name"], items=[{"name": "a|b"}])
        self.assertIn("a\\|b", inventory.render_markdown([section], {}))

    def test_empty_section_renders_none_rather_than_an_empty_table(self) -> None:
        section = inventory.Section(key="t", title="T", columns=["name"], items=[])
        self.assertIn("_none_", inventory.render_markdown([section], {}))


if __name__ == "__main__":
    unittest.main()
