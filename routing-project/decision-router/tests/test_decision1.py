import asyncio

import httpx
import pytest

from decision_router.decision1 import (
    DecisionError,
    build_request,
    build_state,
    execution_order,
    parse_answer,
    rank,
)

OPTIONS = ["gpt-5.5", "o4-mini", "gpt-5.6-terra"]


def answer(**overrides):
    route = {"type": "choice", "choice": "o4-mini",
             "probabilities": {"gpt-5.5": 0.2, "o4-mini": 0.5, "gpt-5.6-terra": 0.3}, "confidence": 0.8}
    route.update(overrides)
    return {"answers": {"route": route}}


def test_request_shape_matches_systemone_choice():
    request = build_request("microsoft-decision-1", "state text", "pick one", {"a": "A", "b": "B"})
    assert request == {
        "model": "microsoft-decision-1",
        "state": "state text",
        "questions": {"route": {"type": "choice", "instructions": "pick one", "criteria": {"a": "A", "b": "B"}}},
    }


def test_parse_valid_answer():
    choice, probabilities, confidence = parse_answer(answer(), OPTIONS)
    assert choice == "o4-mini" and confidence == 0.8 and probabilities["gpt-5.6-terra"] == 0.3


@pytest.mark.parametrize("body, code", [
    ({"answers": {"route": {"type": "refusal"}}}, "refusal"),
    ({"nope": 1}, "invalid_response"),
    (answer(type="score"), "invalid_response"),
    (answer(probabilities={"gpt-5.5": 0.5, "o4-mini": 0.5}), "invalid_response"),
    (answer(probabilities={"gpt-5.5": 0.5, "o4-mini": 0.5, "gpt-5.6-terra": 0.5}), "invalid_response"),
    (answer(probabilities={"gpt-5.5": 0.2, "o4-mini": 0.5, "gpt-5.6-terra": 0.32}), "invalid_response"),
    (answer(probabilities={"gpt-5.5": 0.2, "o4-mini": 0.5, "gpt-5.6-terra": 0.3, "gpt-5-nano": 0.0}), "invalid_response"),
    (answer(choice="gpt-5-nano"), "invalid_response"),
    (answer(probabilities={"gpt-5.5": True, "o4-mini": 0.5, "gpt-5.6-terra": 0.5}), "invalid_response"),
])
def test_invalid_answers_are_rejected(body, code):
    with pytest.raises(DecisionError) as error:
        parse_answer(body, OPTIONS)
    assert error.value.code == code


def test_probabilities_rounded_in_transit_are_renormalised():
    _, probabilities, _ = parse_answer(answer(probabilities={"gpt-5.5": 0.333, "o4-mini": 0.334, "gpt-5.6-terra": 0.331}),
                                       OPTIONS)
    assert abs(sum(probabilities.values()) - 1) < 1e-12


def quality(catalog):
    return [catalog.by_name[n] for n in ("gpt-5.5", "gpt-5.6-sol", "gpt-6-astra")]


def test_ranking_ignores_the_order_options_were_sent_in(catalog):
    probabilities = {"gpt-6-astra": 0.2, "gpt-5.6-sol": 0.5, "gpt-5.5": 0.3}
    assert rank(probabilities, quality(catalog)) == ["gpt-5.6-sol", "gpt-5.5", "gpt-6-astra"]
    assert rank(probabilities, list(reversed(quality(catalog)))) == ["gpt-5.6-sol", "gpt-5.5", "gpt-6-astra"]


def test_confident_top_model_is_called_first():
    order, low = execution_order(["a", "b", "c"], {"a": 0.7, "b": 0.2, "c": 0.1}, 0.5)
    assert order == ["a", "b", "c"] and low is False


def test_low_confidence_calls_the_second_model_first_and_keeps_the_top_as_backup():
    order, low = execution_order(["a", "b", "c"], {"a": 0.45, "b": 0.35, "c": 0.2}, 0.5)
    assert order == ["b", "a", "c"] and low is True


def test_threshold_zero_disables_the_rule():
    assert execution_order(["a", "b"], {"a": 0.3, "b": 0.7 - 0.0001}, 0.0) == (["a", "b"], False)


def test_rank_orders_by_probability_then_price(catalog):
    candidates = quality(catalog)
    assert rank({"gpt-5.5": 0.2, "gpt-5.6-sol": 0.5, "gpt-6-astra": 0.3}, candidates) == ["gpt-5.6-sol", "gpt-6-astra", "gpt-5.5"]
    # tie between gpt-5.5 (tier 18) and gpt-6-astra (tier 24): the cheaper wins
    assert rank({"gpt-5.5": 0.4, "gpt-5.6-sol": 0.2, "gpt-6-astra": 0.4}, candidates)[0] == "gpt-5.5"


def test_state_includes_conversation_and_request_facts():
    body = {"messages": [{"role": "system", "content": "Be brief."},
                         {"role": "user", "content": [{"type": "text", "text": "Describe this"},
                                                      {"type": "image_url", "image_url": {"url": "x"}}]}],
            "tools": [{"type": "function", "function": {"name": "search"}}],
            "max_completion_tokens": 300}
    state = build_state(body, 24_000)
    assert "tools offered: search" in state and "output limit: 300 tokens" in state
    assert "[system]\nBe brief." in state and "[image_url attachment]" in state


def test_long_conversations_keep_system_prompt_and_latest_turns():
    messages = [{"role": "system", "content": "SYSTEM RULES"}]
    messages += [{"role": "user", "content": f"turn {i} " + "x" * 2000} for i in range(30)]
    messages.append({"role": "user", "content": "FINAL QUESTION"})
    state = build_state({"messages": messages}, 10_000)
    assert len(state) <= 10_000
    assert "SYSTEM RULES" in state and "FINAL QUESTION" in state and "earlier message(s) omitted" in state


def test_one_oversized_message_is_clipped_in_the_middle():
    state = build_state({"messages": [{"role": "user", "content": "HEAD" + "y" * 50_000 + "TAIL"}]}, 5_000)
    assert len(state) <= 5_000 and "HEAD" in state and "TAIL" in state and "characters omitted" in state


def test_single_candidate_skips_the_call(catalog, settings):
    from decision_router.auth import FoundryAuth
    from decision_router.decision1 import Decision1Client

    def fail(_):
        raise AssertionError("Decision-1 must not be called with one option")

    client = Decision1Client("https://x/systemone", "d1", FoundryAuth("k"), http=httpx.AsyncClient(transport=httpx.MockTransport(fail)))
    only = quality(catalog)[:1]
    decision = asyncio.run(client.decide("s", "i", catalog.criteria(only), only))
    assert decision.skipped and decision.ranking == ["gpt-5.5"]
