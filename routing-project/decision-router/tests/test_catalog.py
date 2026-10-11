import json

import pytest

from decision_router.catalog import Catalog, CatalogError, Catalogs

DEPRECATED = {"gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano", "gpt-4o", "gpt-4o-mini", "o4-mini",
              "gpt-5", "gpt-5-mini", "gpt-5-nano"}


def test_old_catalog_is_the_model_router_pool_without_deprecated_models(catalog):
    assert catalog.compatibility == "old" and len(catalog.models) == 26
    names = {m.name for m in catalog.models}
    assert not names & DEPRECATED
    assert all(m.in_model_router for m in catalog.models)
    assert {"claude-opus-5", "grok-4.6", "FW-Kimi-K3", "DeepSeek-V3.2", "gpt-6-astra"} <= names


def test_new_catalog_has_latest_models_mai_and_phi(new_catalog):
    names = {m.name for m in new_catalog.models}
    assert {"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "gpt-6.1-sol", "grok-4.7",
            "DeepSeek-V4-Pro", "MAI-Thinking-1", "Phi-4", "Phi-4-reasoning", "Phi-4-mini-instruct"} <= names
    assert not names & DEPRECATED and "claude-haiku-4-5" not in names and "claude-sonnet-4-5" not in names
    assert not new_catalog.by_name["claude-opus-5-5"].in_model_router


def test_entries_carry_api_endpoint_and_hosting(catalog, new_catalog):
    assert catalog.by_name["claude-opus-5"].api == "anthropic_messages"
    assert catalog.by_name["claude-opus-5"].endpoint_path == "/anthropic/v1/messages"
    assert new_catalog.by_name["MAI-Thinking-1"].endpoint_path == "/mai/v1/chat/completions"
    assert catalog.by_name["claude-opus-4-7"].infrastructure == "anthropic"
    assert catalog.by_name["FW-GLM-5.3"].infrastructure == "fireworks"
    assert "swedencentral" in catalog.by_name["gpt-5.4-nano"].regions["global_standard"]
    assert catalog.by_name["gpt-5.4-mini"].max_input_tokens == 272_000


def test_claude_version_sets_hosting(catalog, new_catalog):
    # Foundry: Claude version 1 runs on Anthropic infrastructure, version 2 on Azure (Data Zone only on Azure)
    opus_old, opus_new = catalog.by_name["claude-opus-5"], new_catalog.by_name["claude-opus-5-5"]
    assert (opus_old.version, opus_old.infrastructure, set(opus_old.regions)) == ("1", "anthropic", {"global_standard"})
    assert (opus_new.version, opus_new.infrastructure) == ("2", "azure")
    assert set(opus_new.regions) == {"global_standard", "data_zone_standard"}
    assert catalog.by_name["claude-haiku-4-5"].retires == "2026-11-15"


def test_tiers_follow_prices(settings, catalogs):
    prices = json.loads(settings.pricing_path.read_text())["models"]
    for catalog in catalogs.catalogs.values():
        inputs = [prices[m.name]["input_per_million"] for m in catalog.models]
        assert inputs == sorted(inputs) and [m.tier for m in catalog.models] == list(range(1, len(inputs) + 1))


def test_price_band_takes_the_modes_share(catalog):
    models = catalog.models[:10]
    assert [m.name for m in catalog.price_band("cost", models)] == [m.name for m in models[:4]]
    assert [m.name for m in catalog.price_band("quality", models)] == [m.name for m in models[-4:]]
    assert catalog.price_band("balanced", models) == models
    assert len(catalog.price_band("cost", models[:1])) == 1


def test_criteria_cover_exactly_the_candidates_and_state_price_rank(catalog):
    picked = [catalog.by_name["gpt-5.4-nano"], catalog.by_name["gpt-6-astra"]]
    criteria = catalog.criteria(picked)
    assert list(criteria) == ["gpt-5.4-nano", "gpt-6-astra"]
    assert criteria["gpt-6-astra"].endswith("Price rank 26 of 26 in the pool (1 is cheapest).")


def test_resolve_maps_versioned_and_alias_names(catalog):
    assert catalog.resolve("gpt-5.4-mini-2026-03-17") == "gpt-5.4-mini"
    assert catalog.resolve("grok-4.1-fast-reasoning") == "grok-4-1-fast-reasoning"
    assert catalog.resolve("gpt-4.1") is None and catalog.resolve(None) is None


def test_compatibility_selection(catalogs):
    assert catalogs.get(None).compatibility == "old"
    assert catalogs.get("new").compatibility == "new"
    with pytest.raises(CatalogError):
        catalogs.get("legacy")


def test_deployment_override(settings):
    catalogs = Catalogs.load(settings.catalog_dir, "old", {"gpt-5.5": "prod-gpt55", "claude-opus-5-5": "opus"})
    assert catalogs.get("old").by_name["gpt-5.5"].deployment == "prod-gpt55"
    assert catalogs.get("new").by_name["claude-opus-5-5"].deployment == "opus"
    with pytest.raises(CatalogError):
        Catalogs.load(settings.catalog_dir, "old", {"not-a-model": "x"})


@pytest.mark.parametrize("mutate, message", [
    (lambda d: d["routing_modes"]["cost"].update(price_band={"from": "middle", "share": 0.3}), "price_band"),
    (lambda d: d["routing_modes"].pop("quality"), "exactly"),
    (lambda d: d["models"][1].update(tier=d["models"][0]["tier"]), "tiers must be unique"),
    (lambda d: d["models"][0]["capabilities"].update(tools="maybe"), "yes/no/unknown"),
    (lambda d: d["models"][0].update(api="grpc"), "api must be"),
    (lambda d: d.update(default_mode="fast"), "default_mode"),
])
def test_invalid_catalogs_fail_at_load(settings, tmp_path, mutate, message):
    data = json.loads((settings.catalog_dir / "catalog_old.json").read_text())
    mutate(data)
    path = tmp_path / "catalog_old.json"
    path.write_text(json.dumps(data))
    with pytest.raises(CatalogError, match=message):
        Catalog.load(path)


def test_catalogs_are_in_sync_with_the_spec(settings):
    spec = json.loads((settings.catalog_dir / "catalog_spec.json").read_text())
    for compatibility in ("old", "new"):
        built = json.loads((settings.catalog_dir / f"catalog_{compatibility}.json").read_text())
        wanted = {m["name"]: m["description"] for m in spec["catalogs"][compatibility]["models"]}
        assert {m["name"]: m["description"] for m in built["models"]} == wanted, \
            "catalog_spec.json changed: rerun scripts/build_catalogs.py"


def test_every_model_has_a_price_entry(settings, catalogs):
    prices = json.loads(settings.pricing_path.read_text())["models"]
    for catalog in catalogs.catalogs.values():
        assert {m.name for m in catalog.models} <= set(prices)
