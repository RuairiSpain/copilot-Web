import json

import pytest

from decision_router.catalog import Catalog, CatalogError


def test_stage1_filter_is_deterministic_and_cheapest_first(catalog):
    assert [m.name for m in catalog.candidates("cost")] == ["gpt-5-nano", "gpt-5-mini", "deepseek-v4-flash"]
    assert [m.name for m in catalog.candidates("quality")] == ["gpt-5.5", "o4-mini", "gpt-5.6-terra"]
    assert len(catalog.candidates("balanced")) == 6
    assert catalog.candidates("cost") == catalog.candidates("cost")


def test_unknown_mode_is_rejected(catalog):
    with pytest.raises(CatalogError):
        catalog.candidates("cheap")


def test_criteria_cover_exactly_the_candidates_and_state_price_rank(catalog):
    criteria = catalog.criteria(catalog.candidates("quality"))
    assert list(criteria) == ["gpt-5.5", "o4-mini", "gpt-5.6-terra"]
    assert criteria["gpt-5.6-terra"].endswith("Price rank 6 of 6 in the pool (1 is cheapest).")


def test_resolve_maps_versioned_names_to_pool_names(catalog):
    assert catalog.resolve("gpt-5-mini-2025-08-07") == "gpt-5-mini"
    assert catalog.resolve("GPT-5-NANO") == "gpt-5-nano"
    assert catalog.resolve("gpt-4.1") is None
    assert catalog.resolve(None) is None


def test_deployment_override(settings):
    catalog = Catalog.load(settings.catalog_path, {"gpt-5.5": "prod-gpt55"})
    assert catalog.by_name["gpt-5.5"].deployment == "prod-gpt55"
    assert catalog.resolve("prod-gpt55") == "gpt-5.5"
    with pytest.raises(CatalogError):
        Catalog.load(settings.catalog_path, {"not-a-model": "x"})


@pytest.mark.parametrize("mutate, message", [
    (lambda d: d["routing_modes"]["cost"].update(models=[]), "allows no models"),
    (lambda d: d["routing_modes"]["cost"]["models"].append("ghost"), "unknown models"),
    (lambda d: d["routing_modes"].pop("quality"), "exactly"),
    (lambda d: d["models"][1].update(tier=1), "tiers must be unique"),
    (lambda d: d.update(default_mode="fast"), "default_mode"),
])
def test_invalid_catalogs_fail_at_load(settings, tmp_path, mutate, message):
    data = json.loads(settings.catalog_path.read_text())
    mutate(data)
    path = tmp_path / "catalog.json"
    path.write_text(json.dumps(data))
    with pytest.raises(CatalogError, match=message):
        Catalog.load(path)
