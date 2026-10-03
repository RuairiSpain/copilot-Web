from __future__ import annotations

import json
from typing import Any

import pytest

from intake.brief import VAGUE_PATTERN, ProjectBrief
from intake.gate import check_brief, evaluate_brief


class TestCompleteBrief:
    def test_a_complete_brief_passes(self, brief_dict: dict[str, Any]) -> None:
        result = evaluate_brief(brief_dict)
        assert result.ok
        assert result.issues == ()
        assert isinstance(result.brief, ProjectBrief)

    def test_json_text_is_accepted(self, brief_dict: dict[str, Any]) -> None:
        assert evaluate_brief(json.dumps(brief_dict)).ok

    def test_confirmed_no_dependencies_passes(self, brief_dict: dict[str, Any]) -> None:
        brief_dict["dependencies"] = []
        brief_dict["no_dependencies_confirmed"] = True
        assert evaluate_brief(brief_dict).ok

    def test_render_for_a_complete_brief_asks_for_confirmation(
        self, brief_dict: dict[str, Any]
    ) -> None:
        assert "confirmation" in evaluate_brief(brief_dict).render()


class TestIncompleteBriefs:
    @pytest.mark.parametrize(
        "field",
        [
            "project_name",
            "summary",
            "target_audience",
            "functional_requirements",
            "io_samples",
            "out_of_scope",
            "definition_of_done",
        ],
    )
    def test_each_required_field_is_enforced(self, brief_dict: dict[str, Any], field: str) -> None:
        del brief_dict[field]
        result = evaluate_brief(brief_dict)
        assert not result.ok
        assert any(issue.startswith(field) for issue in result.issues)

    @pytest.mark.parametrize("field", ["functional_requirements", "io_samples", "out_of_scope"])
    def test_empty_lists_are_rejected(self, brief_dict: dict[str, Any], field: str) -> None:
        brief_dict[field] = []
        assert not evaluate_brief(brief_dict).ok

    def test_invalid_json_is_reported(self) -> None:
        result = evaluate_brief("{not json")
        assert not result.ok
        assert "not valid JSON" in result.issues[0]

    def test_a_json_array_is_not_a_brief(self) -> None:
        assert evaluate_brief("[1, 2]").issues == ("The brief must be a JSON object.",)

    def test_unknown_fields_are_rejected(self, brief_dict: dict[str, Any]) -> None:
        brief_dict["priority"] = "high"
        assert not evaluate_brief(brief_dict).ok

    @pytest.mark.parametrize("name", ["Slugify", "1abc", "a", "has-dash", "x" * 41])
    def test_project_name_must_be_a_package_name(
        self, brief_dict: dict[str, Any], name: str
    ) -> None:
        brief_dict["project_name"] = name
        assert not evaluate_brief(brief_dict).ok

    def test_missing_dependencies_must_be_confirmed_as_none(
        self, brief_dict: dict[str, Any]
    ) -> None:
        brief_dict["dependencies"] = []
        result = evaluate_brief(brief_dict)
        assert any("no_dependencies_confirmed" in issue for issue in result.issues)

    def test_contradictory_dependency_answers(self, brief_dict: dict[str, Any]) -> None:
        brief_dict["no_dependencies_confirmed"] = True
        assert any("true but dependencies" in i for i in evaluate_brief(brief_dict).issues)

    def test_done_must_mention_tests(self, brief_dict: dict[str, Any]) -> None:
        brief_dict["definition_of_done"] = ["The library is published."]
        assert any("automated tests" in i for i in evaluate_brief(brief_dict).issues)

    def test_duplicate_requirements(self, brief_dict: dict[str, Any]) -> None:
        brief_dict["functional_requirements"] = ["The function returns a slug."] * 2
        assert any("duplicates" in i for i in evaluate_brief(brief_dict).issues)

    def test_requirements_must_be_long_enough_to_test(self, brief_dict: dict[str, Any]) -> None:
        brief_dict["functional_requirements"] = ["Be fast."]
        assert any("too short" in i for i in evaluate_brief(brief_dict).issues)

    def test_render_lists_every_open_point(self, brief_dict: dict[str, Any]) -> None:
        brief_dict["dependencies"] = []
        brief_dict["definition_of_done"] = ["Done."]
        text = evaluate_brief(brief_dict).render()
        assert "NOT ready" in text
        assert text.count("\n- ") >= 2


class TestVagueness:
    @pytest.mark.parametrize(
        "phrase",
        [
            "parses dates, etc.",
            "handles various formats",
            "TBD",
            "as needed",
            "user-friendly output",
            "something like JSON",
            "maybe supports YAML",
            "and so on",
            "appropriate errors",
        ],
    )
    def test_vague_phrases_are_flagged(self, brief_dict: dict[str, Any], phrase: str) -> None:
        brief_dict["target_audience"] = f"Developers who want it, {phrase} for them."
        issues = evaluate_brief(brief_dict).issues
        assert any("target_audience is vague" in i for i in issues)

    @pytest.mark.parametrize("text", ["fetch the page", "Etcetera Corp", "variously", "stuffing"])
    def test_ordinary_words_are_not_flagged(self, text: str) -> None:
        assert VAGUE_PATTERN.search(text) is None

    def test_every_text_field_is_scanned(self, brief_dict: dict[str, Any]) -> None:
        brief_dict["out_of_scope"] = ["Anything else, etc."]
        brief_dict["definition_of_done"] = ["Tests pass as needed."]
        issues = check_brief(ProjectBrief.model_validate(brief_dict))
        assert any(i.startswith("out_of_scope[0]") for i in issues)
        assert any(i.startswith("definition_of_done[0]") for i in issues)


class TestMarkdown:
    def test_contains_every_section(self, brief_dict: dict[str, Any]) -> None:
        text = ProjectBrief.model_validate(brief_dict).to_markdown()
        for heading in (
            "# Project brief: slugify_lib",
            "## Target audience",
            "## Functional requirements",
            "## Dependencies",
            "## Input and output samples",
            "## Out of scope",
            "## Definition of done",
        ):
            assert heading in text
        assert "hello-world" in text
        assert "- [ ] All unit tests pass." in text

    def test_says_so_when_there_are_no_dependencies(self, brief_dict: dict[str, Any]) -> None:
        brief_dict["dependencies"] = []
        brief_dict["no_dependencies_confirmed"] = True
        assert "None beyond Python" in ProjectBrief.model_validate(brief_dict).to_markdown()
