"""Scheduler strategies, including property tests."""

from __future__ import annotations

from datetime import UTC, datetime, timedelta

from hypothesis import given
from hypothesis import strategies as st

from hosted_agent_kit.domain.enums import (
    FoundrySessionStatus,
    LocalSessionState,
    SchedulerStrategy,
)
from hosted_agent_kit.domain.models import SessionRecord
from hosted_agent_kit.services.scheduler import Scheduler, idle_since, order_key

T0 = datetime(2026, 1, 1, tzinfo=UTC)
S = SchedulerStrategy


def rec(sid: str, created: int = 0, released: int | None = None) -> SessionRecord:
    return SessionRecord(
        session_id=sid,
        agent_name="a",
        platform_status=FoundrySessionStatus.ACTIVE,
        local_state=LocalSessionState.AVAILABLE,
        created_at=T0 + timedelta(seconds=created),
        last_seen_at=T0,
        last_released_at=None if released is None else T0 + timedelta(seconds=released),
    )


def pick(scheduler: Scheduler, strategy: S, records: list[SessionRecord]) -> str | None:
    chosen = scheduler.select(strategy, "a", records)
    return chosen.session_id if chosen else None


def test_no_eligible_sessions_returns_none_for_every_strategy() -> None:
    scheduler = Scheduler()
    assert all(pick(scheduler, s, []) is None for s in S)


def test_first_available_uses_stable_registry_order_regardless_of_input_order() -> None:
    records = [rec("c", 3), rec("a", 1), rec("b", 2)]
    assert pick(Scheduler(), S.FIRST_AVAILABLE, records) == "a"
    assert pick(Scheduler(), S.FIRST_AVAILABLE, list(reversed(records))) == "a"


def test_first_available_breaks_creation_ties_by_session_id() -> None:
    assert pick(Scheduler(), S.FIRST_AVAILABLE, [rec("z", 1), rec("m", 1)]) == "m"


def test_oldest_idle_picks_oldest_release_and_uses_creation_when_never_released() -> None:
    records = [rec("a", 1, released=50), rec("b", 2, released=10), rec("c", 3)]
    # c was never released, so it counts from creation (t=3), the oldest of all
    assert pick(Scheduler(), S.OLDEST_IDLE, records) == "c"
    records = [rec("a", 1, released=50), rec("b", 2, released=10)]
    assert pick(Scheduler(), S.OLDEST_IDLE, records) == "b"


def test_newest_idle_picks_latest_release() -> None:
    records = [rec("a", 1, released=50), rec("b", 2, released=10), rec("c", 3, released=70)]
    assert pick(Scheduler(), S.NEWEST_IDLE, records) == "c"


def test_idle_ties_break_by_registry_order() -> None:
    records = [rec("b", 2, released=10), rec("a", 1, released=10)]
    assert pick(Scheduler(), S.OLDEST_IDLE, records) == "a"
    assert pick(Scheduler(), S.NEWEST_IDLE, records) == "a"


def test_round_robin_rotates_and_wraps() -> None:
    scheduler = Scheduler()
    records = [rec("a", 1), rec("b", 2), rec("c", 3)]
    assert [pick(scheduler, S.ROUND_ROBIN, records) for _ in range(5)] == ["a", "b", "c", "a", "b"]


def test_round_robin_skipped_busy_sessions_do_not_consume_a_turn() -> None:
    scheduler = Scheduler()
    all_three = [rec("a", 1), rec("b", 2), rec("c", 3)]
    assert pick(scheduler, S.ROUND_ROBIN, all_three) == "a"
    # b is busy and therefore not offered: the turn goes to c, then wraps to a, then b.
    assert pick(scheduler, S.ROUND_ROBIN, [all_three[0], all_three[2]]) == "c"
    assert pick(scheduler, S.ROUND_ROBIN, all_three) == "a"
    assert pick(scheduler, S.ROUND_ROBIN, all_three) == "b"


def test_round_robin_cursor_is_per_agent() -> None:
    scheduler = Scheduler()
    records = [rec("a", 1), rec("b", 2)]
    assert scheduler.select(S.ROUND_ROBIN, "x", records).session_id == "a"  # type: ignore[union-attr]
    assert scheduler.select(S.ROUND_ROBIN, "y", records).session_id == "a"  # type: ignore[union-attr]
    assert scheduler.select(S.ROUND_ROBIN, "x", records).session_id == "b"  # type: ignore[union-attr]


def test_round_robin_survives_removal_of_the_last_chosen_session() -> None:
    scheduler = Scheduler()
    assert pick(scheduler, S.ROUND_ROBIN, [rec("a", 1), rec("b", 2)]) == "a"
    assert pick(scheduler, S.ROUND_ROBIN, [rec("b", 2), rec("c", 3)]) == "b"
    assert pick(scheduler, S.ROUND_ROBIN, [rec("c", 3)]) == "c"
    assert pick(scheduler, S.ROUND_ROBIN, [rec("a", 1)]) == "a"  # wraps when nothing is after


def test_idle_since_prefers_release_time() -> None:
    assert idle_since(rec("a", 1, released=9)) == T0 + timedelta(seconds=9)
    assert idle_since(rec("a", 1)) == T0 + timedelta(seconds=1)
    assert order_key(rec("a", 1)) < order_key(rec("a", 2))


records_strategy = st.lists(
    st.tuples(
        st.integers(0, 20),
        st.one_of(st.none(), st.integers(0, 100)),
    ),
    min_size=1,
    max_size=12,
    unique_by=lambda item: item,
).map(lambda items: [rec(f"s{index:02d}", c, r) for index, (c, r) in enumerate(items)])


@given(records_strategy, st.sampled_from(list(S)))
def test_selection_is_always_a_member_and_independent_of_input_order(
    records: list[SessionRecord], strategy: S
) -> None:
    first = Scheduler().select(strategy, "a", records)
    second = Scheduler().select(strategy, "a", list(reversed(records)))
    assert first is not None and second is not None
    assert first.session_id in {r.session_id for r in records}
    assert first.session_id == second.session_id


@given(records_strategy)
def test_round_robin_visits_every_session_before_repeating(records: list[SessionRecord]) -> None:
    scheduler = Scheduler()
    picks = [scheduler.select(S.ROUND_ROBIN, "a", records).session_id for _ in records]  # type: ignore[union-attr]
    assert sorted(picks) == sorted(r.session_id for r in records)


@given(records_strategy)
def test_oldest_is_never_newer_than_newest(records: list[SessionRecord]) -> None:
    oldest = Scheduler().select(S.OLDEST_IDLE, "a", records)
    newest = Scheduler().select(S.NEWEST_IDLE, "a", records)
    assert oldest is not None and newest is not None
    assert idle_since(oldest) <= idle_since(newest)
