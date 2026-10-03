"""Check several kits' configuration files together: ownership overlaps and quota budgets.

Each kit has its own YAML file. Mistakes only show when the files are read side by side: two kits
that schedule the same agent, or budgets that add up to more than the region allows. ``plan`` reads
the files, never connects to Foundry, and returns problems as text. ``hack plan`` prints them.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from hosted_agent_kit.config.loader import build_config, build_settings_overrides, parse_yaml
from hosted_agent_kit.config.models import ConfigError
from hosted_agent_kit.config.settings import QuotaSettings, ShardSettings


@dataclass(frozen=True)
class KitPlan:
    source: str
    kit_id: str
    project: str
    agents: tuple[str, ...]
    shard: ShardSettings | None
    quota: QuotaSettings

    @property
    def scope(self) -> tuple[str, str] | None:
        q = self.quota
        if q.subscription_id and q.region:
            return (q.subscription_id, q.region)
        return None


@dataclass
class Plan:
    kits: list[KitPlan] = field(default_factory=list)
    problems: list[str] = field(default_factory=list)
    notes: list[str] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        return not self.problems


def load_kit(path: Path) -> KitPlan:
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as exc:
        raise ConfigError(f"cannot read file: {exc.strerror}") from exc
    raw = parse_yaml(text, str(path))
    config = build_config(raw, str(path))
    overrides = build_settings_overrides(raw, str(path))
    owns = overrides.get("owns")
    unknown = [n for n in owns or [] if n not in config.agents]
    if unknown:
        raise ConfigError(
            f"{path}: owns names agents that are not configured: {', '.join(unknown)}"
        )
    agents = tuple(n for n in config.names if owns is None or n in owns)
    shard = overrides.get("shard")
    return KitPlan(
        source=str(path),
        kit_id=str(overrides.get("kit_id") or path.stem),
        project=str(overrides.get("foundry_project_endpoint") or "(this project)"),
        agents=agents,
        shard=ShardSettings(**shard) if isinstance(shard, dict) else None,
        quota=QuotaSettings(**(overrides.get("quota") or {})),
    )


def plan(paths: list[Path]) -> Plan:
    result = Plan()
    for path in paths:
        try:
            result.kits.append(load_kit(path))
        except (ConfigError, OSError, ValueError) as exc:
            result.problems.append(f"{path}: {exc}")
    _check_ids(result)
    _check_ownership(result)
    _check_quota(result)
    return result


def _check_ids(result: Plan) -> None:
    seen: dict[str, str] = {}
    for kit in result.kits:
        if kit.kit_id in seen:
            result.problems.append(
                f"kit_id '{kit.kit_id}' is used by {seen[kit.kit_id]} and {kit.source}: "
                "each kit needs its own id"
            )
        seen[kit.kit_id] = kit.source


def _check_ownership(result: Plan) -> None:
    owners: dict[tuple[str, str], list[KitPlan]] = {}
    for kit in result.kits:
        for agent in kit.agents:
            owners.setdefault((kit.project, agent), []).append(kit)
    for (project, agent), kits in sorted(owners.items()):
        if len(kits) < 2:
            continue
        names = ", ".join(k.kit_id for k in kits)
        shards = [k.shard for k in kits]
        if any(s is None for s in shards):
            result.problems.append(
                f"agent '{agent}' ({project}) is scheduled by {names}, and at least one of them "
                "is not sharded, so they overlap"
            )
            continue
        counts = {s.count for s in shards if s is not None}
        indexes = [s.index for s in shards if s is not None]
        if len(counts) > 1:
            result.problems.append(
                f"agent '{agent}' ({project}) is sharded with different counts by {names}"
            )
        elif len(set(indexes)) != len(indexes):
            result.problems.append(
                f"agent '{agent}' ({project}) has two kits on the same shard: {names}"
            )
        elif len(indexes) < next(iter(counts)):
            missing = sorted(set(range(next(iter(counts)))) - set(indexes))
            result.notes.append(
                f"agent '{agent}' ({project}): shards {missing} of {next(iter(counts))} "
                "have no kit in these files"
            )


def _check_quota(result: Plan) -> None:
    by_scope: dict[tuple[str, str], list[KitPlan]] = {}
    for kit in result.kits:
        if kit.scope is not None:
            by_scope.setdefault(kit.scope, []).append(kit)
        elif kit.quota.budget is not None:
            result.notes.append(
                f"{kit.kit_id}: has a budget but no quota.subscription_id and quota.region, "
                "so it is not counted against a region limit"
            )
    for scope, kits in sorted(by_scope.items()):
        limits = {k.quota.region_limit for k in kits if k.quota.region_limit is not None}
        spares = {k.quota.spare for k in kits}
        label = f"{scope[0]}/{scope[1]}"
        if len(limits) > 1:
            result.problems.append(f"{label}: the kits disagree on quota.region_limit: {limits}")
            continue
        if len(spares) > 1:
            result.problems.append(f"{label}: the kits disagree on quota.spare: {spares}")
            continue
        budgets = {k.kit_id: k.quota.budget for k in kits}
        unbudgeted = [kit for kit, budget in budgets.items() if budget is None]
        total = sum(b for b in budgets.values() if b is not None) + next(iter(spares))
        if not limits:
            result.notes.append(f"{label}: no quota.region_limit set; budgets total {total}")
            continue
        limit = next(iter(limits))
        if total > limit:
            result.problems.append(
                f"{label}: budgets plus spare total {total}, over the region limit {limit}"
            )
        else:
            result.notes.append(f"{label}: budgets plus spare total {total} of {limit}")
        if unbudgeted:
            result.notes.append(
                f"{label}: no budget for {', '.join(unbudgeted)}; they can use any unallocated "
                "sessions until Foundry refuses"
            )


def render(result: Plan) -> str:
    lines = [f"{len(result.kits)} kit(s)"]
    for kit in result.kits:
        shard = f" shard {kit.shard.index}/{kit.shard.count}" if kit.shard else ""
        budget = kit.quota.budget if kit.quota.budget is not None else "none"
        lines.append(f"  {kit.kit_id}{shard}: {', '.join(kit.agents)} (budget {budget})")
    lines += [f"note: {n}" for n in result.notes]
    lines += [f"PROBLEM: {p}" for p in result.problems]
    return "\n".join(lines)


def to_dict(result: Plan) -> dict[str, Any]:
    return {
        "ok": result.ok,
        "kits": [
            {"id": k.kit_id, "agents": list(k.agents), "budget": k.quota.budget}
            for k in result.kits
        ],
        "notes": result.notes,
        "problems": result.problems,
    }
