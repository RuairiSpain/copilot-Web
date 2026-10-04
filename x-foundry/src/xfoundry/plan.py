"""DeploymentPlan: the validated, normalised and ordered output of Phase 1."""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from pydantic import Field

from xfoundry.diagnostics import Diagnostic, Severity, ValidationFailed, errors_only
from xfoundry.graph import DeploymentGraph, build_graph
from xfoundry.normalise import NormalisedConfig, normalise
from xfoundry.parser import ParsedDocument, parse_file, parse_mapping, parse_text
from xfoundry.schema import SCHEMA_VERSION
from xfoundry.schema.models import Model
from xfoundry.validators import validate_declared, validate_effective


class PlanNode(Model):
    id: str
    kind: str
    stage: int
    scope: str | None = None
    existing: bool = False
    depends_on: list[str] = Field(default_factory=list)


class PlanDiagnostic(Model):
    code: str
    severity: str
    path: str = ""
    message: str


class DeploymentPlan(Model):
    """Everything later phases need: normalised configuration and ordered resource graph."""

    schema_version: str = SCHEMA_VERSION
    config: NormalisedConfig
    nodes: list[PlanNode]
    layers: list[list[str]]
    warnings: list[PlanDiagnostic] = Field(default_factory=list)

    @property
    def order(self) -> list[str]:
        """Node ids with every dependency before its dependents."""
        return [n.id for n in self.nodes]

    def node(self, node_id: str) -> PlanNode:
        return next(n for n in self.nodes if n.id == node_id)

    def to_json(self, indent: int | None = 2) -> str:
        return self.model_dump_json(by_alias=True, exclude_none=True, indent=indent)


@dataclass(slots=True)
class Analysis:
    """Non-raising result: a plan when there are no errors, plus every diagnostic."""

    plan: DeploymentPlan | None
    diagnostics: list[Diagnostic] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        return self.plan is not None

    def raise_for_errors(self) -> DeploymentPlan:
        if self.plan is None:
            raise ValidationFailed(self.diagnostics)
        return self.plan


def _plan(
    config: NormalisedConfig, graph: DeploymentGraph, warnings: list[Diagnostic]
) -> DeploymentPlan:
    order = graph.topological_order()
    nodes = [
        PlanNode(
            id=i,
            kind=graph.nodes[i].kind,
            stage=graph.nodes[i].stage,
            scope=graph.nodes[i].scope,
            existing=graph.nodes[i].existing,
            depends_on=graph.dependencies_of(i),
        )
        for i in order
    ]
    return DeploymentPlan(
        config=config,
        nodes=nodes,
        layers=graph.layers(),
        warnings=[PlanDiagnostic(**d.to_dict()) for d in warnings],
    )


def analyse_parsed(parsed: ParsedDocument) -> Analysis:
    """Validate, normalise and plan an already-parsed document."""
    diagnostics = validate_declared(parsed.config)
    if errors_only(diagnostics):
        return Analysis(None, diagnostics)
    result = normalise(parsed.config)
    diagnostics += result.diagnostics
    if errors_only(diagnostics):
        return Analysis(None, diagnostics)
    diagnostics += validate_effective(result.config, parsed.config)
    if errors_only(diagnostics):
        return Analysis(None, diagnostics)
    warnings = [d for d in diagnostics if d.severity is Severity.WARNING]
    return Analysis(_plan(result.config, build_graph(result.config), warnings), diagnostics)


def _analyse(parse: Any, *args: Any) -> Analysis:
    try:
        parsed = parse(*args)
    except ValidationFailed as exc:
        return Analysis(None, exc.diagnostics)
    return analyse_parsed(parsed)


def analyse_text(text: str, source: str = "<string>") -> Analysis:
    return _analyse(parse_text, text, source)


def analyse_file(path: str | Path) -> Analysis:
    return _analyse(parse_file, path)


def analyse_mapping(document: Any, source: str = "<mapping>") -> Analysis:
    return _analyse(parse_mapping, document, source)


def build_plan(source: str | Path | dict[str, Any]) -> DeploymentPlan:
    """Parse ``source`` (a path, YAML text or loaded ``azure.yaml`` mapping) into a plan.

    Raises :class:`ValidationFailed` when the configuration has errors.
    """
    if isinstance(source, dict):
        return analyse_mapping(source).raise_for_errors()
    if isinstance(source, Path) or (
        isinstance(source, str) and "\n" not in source and Path(source).is_file()
    ):
        return analyse_file(source).raise_for_errors()
    return analyse_text(str(source)).raise_for_errors()
