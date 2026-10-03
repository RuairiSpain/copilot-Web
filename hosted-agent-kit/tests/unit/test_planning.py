"""``hack plan``: ownership overlaps and quota budgets across several kit files."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from hosted_agent_kit.cli import main as cli_main
from hosted_agent_kit.config.loader import build_settings_overrides
from hosted_agent_kit.config.models import ConfigError
from hosted_agent_kit.planning import plan, render

AGENTS = """agentPool:
  agents:
    chat: {mode: stateless}
    memo: {mode: stateful}
    docs: {mode: stateless}
"""


def kit(tmp_path: Path, name: str, hack: str = "") -> Path:
    path = tmp_path / f"{name}.yaml"
    path.write_text(f"hack:\n{hack}\n{AGENTS}" if hack else AGENTS)
    return path


def test_disjoint_kits_with_budgets_inside_the_limit_are_fine(tmp_path: Path) -> None:
    a = kit(
        tmp_path,
        "team-a",
        "  kit_id: team-a\n  owns: [chat]\n  quota: {budget: 300, subscription_id: s1, "
        "region: swedencentral, region_limit: 1000, spare: 100, ledger: {backend: redis}}",
    )
    b = kit(
        tmp_path,
        "team-b",
        "  kit_id: team-b\n  owns: [memo, docs]\n  quota: {budget: 500, subscription_id: s1, "
        "region: swedencentral, region_limit: 1000, spare: 100, ledger: {backend: redis}}",
    )
    result = plan([a, b])
    assert result.ok, result.problems
    assert any("900 of 1000" in note for note in result.notes)
    assert "team-a: chat (budget 300)" in render(result)


def test_two_kits_scheduling_the_same_agent_overlap(tmp_path: Path) -> None:
    a = kit(tmp_path, "a", "  kit_id: a\n  owns: [chat, memo]")
    b = kit(tmp_path, "b", "  kit_id: b\n  owns: [memo]")
    result = plan([a, b])
    assert not result.ok
    assert any("'memo'" in p and "overlap" in p for p in result.problems)


def test_a_kit_without_owns_owns_everything_so_it_overlaps_any_other(tmp_path: Path) -> None:
    result = plan(
        [kit(tmp_path, "a", "  kit_id: a"), kit(tmp_path, "b", "  kit_id: b\n  owns: [chat]")]
    )
    assert not result.ok


def test_shards_of_one_agent_do_not_overlap_but_gaps_and_clashes_are_reported(
    tmp_path: Path,
) -> None:
    def shard(index: int, count: int, name: str) -> Path:
        return kit(
            tmp_path,
            name,
            f"  kit_id: {name}\n  owns: [memo]\n  shard: {{index: {index}, count: {count}}}",
        )

    ok = plan([shard(0, 2, "s0"), shard(1, 2, "s1")])
    assert ok.ok and not ok.notes
    gap = plan([shard(0, 3, "g0"), shard(1, 3, "g1")])
    assert gap.ok and any("shards [2] of 3" in n for n in gap.notes)
    clash = plan([shard(0, 2, "c0"), shard(0, 2, "c0b")])
    assert any("same shard" in p for p in clash.problems)
    counts = plan([shard(0, 2, "d0"), shard(1, 3, "d1")])
    assert any("different counts" in p for p in counts.problems)


def test_a_sharded_kit_and_an_unsharded_one_overlap(tmp_path: Path) -> None:
    a = kit(tmp_path, "a", "  kit_id: a\n  owns: [memo]\n  shard: {index: 0, count: 2}")
    b = kit(tmp_path, "b", "  kit_id: b\n  owns: [memo]")
    assert not plan([a, b]).ok


def test_budgets_over_the_region_limit_are_a_problem(tmp_path: Path) -> None:
    quota = "quota: {{budget: {b}, subscription_id: s1, region: r, region_limit: 1000}}"
    a = kit(tmp_path, "a", f"  kit_id: a\n  owns: [chat]\n  {quota.format(b=600)}")
    b = kit(tmp_path, "b", f"  kit_id: b\n  owns: [memo]\n  {quota.format(b=500)}")
    result = plan([a, b])
    assert any("1100, over the region limit 1000" in p for p in result.problems)


def test_disagreeing_limits_and_missing_scope_are_reported(tmp_path: Path) -> None:
    a = kit(
        tmp_path,
        "a",
        "  kit_id: a\n  owns: [chat]\n"
        "  quota: {budget: 10, subscription_id: s1, region: r, region_limit: 1000}",
    )
    b = kit(
        tmp_path,
        "b",
        "  kit_id: b\n  owns: [memo]\n"
        "  quota: {budget: 10, subscription_id: s1, region: r, region_limit: 2000}",
    )
    assert any("disagree" in p for p in plan([a, b]).problems)
    c = kit(tmp_path, "c", "  kit_id: c\n  owns: [docs]\n  quota: {budget: 10}")
    assert any("not counted" in n for n in plan([c]).notes)


def test_duplicate_ids_unknown_agents_and_bad_files_are_problems(tmp_path: Path) -> None:
    a = kit(tmp_path, "a", "  kit_id: same\n  owns: [chat]")
    b = kit(tmp_path, "b", "  kit_id: same\n  owns: [memo]")
    assert any("same" in p and "own id" in p for p in plan([a, b]).problems)
    nope = kit(tmp_path, "nope", "  owns: [ghost]")
    assert any("ghost" in p for p in plan([nope]).problems)
    assert any("cannot read" in p for p in plan([tmp_path / "missing.yaml"]).problems)


def test_connection_strings_are_refused_in_the_file() -> None:
    with pytest.raises(ConfigError, match=r"POOL_QUOTA__LEDGER__URL"):
        build_settings_overrides({"hack": {"quota": {"ledger": {"url": "rediss://x"}}}})
    with pytest.raises(ConfigError, match=r"POOL_OWNERSHIP__URL"):
        build_settings_overrides({"hack": {"ownership": {"url": "rediss://x"}}})
    with pytest.raises(ConfigError, match="secret"):
        build_settings_overrides({"hack": {"user_isolation_secret": "x" * 40}})


def test_the_command_prints_and_sets_the_exit_status(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    good = kit(tmp_path, "good", "  kit_id: good\n  owns: [chat]")
    assert cli_main(["plan", str(good)]) == 0
    assert "good: chat" in capsys.readouterr().out
    clash = kit(tmp_path, "clash", "  kit_id: clash\n  owns: [chat]")
    assert cli_main(["plan", "--json", str(good), str(clash)]) == 1
    data = json.loads(capsys.readouterr().out)
    assert data["ok"] is False and data["problems"]
