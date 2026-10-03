"""The project brief: the single, unambiguous definition of what the team must build.

The lead agent interviews the user until every field here is filled in. The brief is a
Pydantic model so the same definition drives three things: the JSON schema shown to the
model, the validation the gate applies, and the markdown the planner reads.
"""

from __future__ import annotations

import re
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

# Words that signal an unfinished thought. A brief containing them is not ambiguity-free.
VAGUE_PATTERN = re.compile(
    r"\b(?:etc|and so on|tbd|to be decided|as needed|as required|something like|maybe|"
    r"probably|various|stuff|and more|appropriate|appropriately|user[- ]friendly)\b",
    re.IGNORECASE,
)
TESTS_PATTERN = re.compile(r"\btests?\b", re.IGNORECASE)

DependencyKind = Literal["library", "service", "api", "database", "tool", "other"]


class Dependency(BaseModel):
    """Something the project relies on that it does not build itself."""

    model_config = ConfigDict(extra="forbid", str_strip_whitespace=True)

    name: str = Field(min_length=1, description="Package, service or API name.")
    kind: DependencyKind = Field(description="What sort of dependency this is.")
    purpose: str = Field(min_length=5, description="Why the project needs it.")


class IOSample(BaseModel):
    """One concrete example of an input and the exact output expected for it."""

    model_config = ConfigDict(extra="forbid", str_strip_whitespace=True)

    name: str = Field(min_length=1, description="Short label for the example.")
    input: str = Field(min_length=1, description="The input, verbatim.")
    expected_output: str = Field(min_length=1, description="The exact expected output.")


class ProjectBrief(BaseModel):
    """Everything the team needs to build the project without asking again."""

    model_config = ConfigDict(extra="forbid", str_strip_whitespace=True)

    project_name: str = Field(
        pattern=r"^[a-z][a-z0-9_]{1,39}$",
        description="Python package name: lower case, digits and underscores, 2 to 40 characters.",
    )
    summary: str = Field(
        min_length=20, description="What the project does, in one or two sentences."
    )
    target_audience: str = Field(
        min_length=10, description="Who will use it, and in what situation."
    )
    functional_requirements: list[str] = Field(
        min_length=1,
        description="One testable statement per item, each describing a single behaviour.",
    )
    dependencies: list[Dependency] = Field(
        default_factory=list,
        description="Libraries, services and APIs the project depends on.",
    )
    no_dependencies_confirmed: bool = Field(
        default=False,
        description="True only if the user said the project has no dependencies beyond Python.",
    )
    io_samples: list[IOSample] = Field(
        min_length=1, description="At least one concrete input and its exact expected output."
    )
    out_of_scope: list[str] = Field(
        min_length=1, description="Things the project must explicitly not do."
    )
    definition_of_done: list[str] = Field(
        min_length=1,
        description="Observable conditions that must all hold. At least one must mention tests.",
    )

    def to_markdown(self) -> str:
        """Render the brief as the document the planner and writer read."""
        lines = [
            f"# Project brief: {self.project_name}",
            "",
            "## Summary",
            self.summary,
            "",
            "## Target audience",
            self.target_audience,
            "",
            "## Functional requirements",
            *[f"{i}. {text}" for i, text in enumerate(self.functional_requirements, 1)],
            "",
            "## Dependencies",
        ]
        if self.dependencies:
            lines += [f"- **{d.name}** ({d.kind}): {d.purpose}" for d in self.dependencies]
        else:
            lines.append("None beyond Python itself (confirmed by the user).")
        lines += ["", "## Input and output samples"]
        for sample in self.io_samples:
            lines += [
                f"### {sample.name}",
                "Input:",
                "```",
                sample.input,
                "```",
                "Expected output:",
                "```",
                sample.expected_output,
                "```",
            ]
        lines += [
            "",
            "## Out of scope",
            *[f"- {item}" for item in self.out_of_scope],
            "",
            "## Definition of done",
            *[f"- [ ] {item}" for item in self.definition_of_done],
            "",
        ]
        return "\n".join(lines)
