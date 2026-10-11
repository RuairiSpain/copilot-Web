#!/usr/bin/env python3
"""Build config/catalog_old.json and config/catalog_new.json.

Inputs:
  - config/catalog_spec.json: which models are in each catalog, descriptions, API type, and
    values the docs do not publish (each marked with its source).
  - config/model_pricing.json: USD list prices. `tier` is computed from them (1 = cheapest).
  - A checkout of MicrosoftDocs/azure-ai-docs at the commit pinned in the spec: lifecycle and
    retirement dates, Model Router membership, regions per deployment type, limits and capabilities.

  git clone --filter=blob:none --sparse https://github.com/MicrosoftDocs/azure-ai-docs.git /tmp/azure-ai-docs
  git -C /tmp/azure-ai-docs sparse-checkout set articles/foundry
  git -C /tmp/azure-ai-docs checkout <commit from catalog_spec.json>
  python scripts/build_catalogs.py --docs /tmp/azure-ai-docs

The script fails loudly when a model in the spec cannot be found in the docs it is told to read.
"""
from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from collections import defaultdict
from datetime import date
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
UNKNOWN = "unknown"
CAPABILITIES = ("tools", "parallel_tools", "structured_output", "json_object", "streaming", "reasoning",
                "image_input", "computer_use")

FILES = {
    "router": "articles/foundry/openai/includes/model-router-supported.md",
    "retirements": "articles/foundry/openai/includes/concepts-model-retirement-schedule-content.md",
    "regions_standard": "articles/foundry/foundry-models/includes/model-matrix/deployments-standard.md",
    "regions_marketplace": "articles/foundry/foundry-models/includes/model-matrix/marketplace-deployments-standard.md",
    "openai": "articles/foundry/openai/includes/models-azure-direct-openai.md",
    "others": "articles/foundry/foundry-models/includes/models-azure-direct-others.md",
    "partners": "articles/foundry/foundry-models/includes/models-partners.md",
    "claude": "articles/foundry/foundry-models/includes/concepts-claude-models-content.md",
}
SECTION_TYPES = {"global standard": "global_standard", "data zone standard": "data_zone_standard",
                 "standard/regional": "standard"}


def clean(cell: str) -> str:
    return re.sub(r"\*\*|`", "", cell).strip()


def tables(text: str):
    """Yield (section heading, header cells, rows) for every markdown table."""
    lines = text.splitlines()
    section = ""
    i = 0
    while i < len(lines):
        line = lines[i]
        if re.match(r"^#{2,3} ", line):
            section = line.lstrip("#").strip().lower()
        if line.startswith("|") and i + 1 < len(lines) and re.match(r"^\|\s*:?-{2,}", lines[i + 1]):
            header = [clean(c) for c in line.strip().strip("|").split("|")]
            rows = []
            i += 2
            while i < len(lines) and lines[i].startswith("|"):
                rows.append([c.strip() for c in lines[i].strip().strip("|").split("|")])
                i += 1
            yield section, header, rows
            continue
        i += 1


def first_name(cell: str) -> str:
    match = re.search(r"`([^`]+)`", cell)
    return (match.group(1) if match else clean(re.sub(r"<[^>]+>", " ", cell)).split()[0]).strip()


def number(text: str) -> int | None:
    match = re.search(r"(\d[\d,]*(?:\.\d+)?)\s*([KkMm]?)\b", text)
    if not match:
        return None
    value = float(match.group(1).replace(",", ""))
    return int(value * {"k": 1_000, "m": 1_000_000}.get(match.group(2).lower(), 1))


class Docs:
    def __init__(self, root: Path):
        self.root = root
        self.text = {key: (root / path).read_text(encoding="utf-8") for key, path in FILES.items()}
        self.commit = subprocess.run(["git", "-C", str(root), "rev-parse", "HEAD"], capture_output=True,
                                     text=True).stdout.strip() or None
        self.router = self._router()
        self.retirements = self._retirements()
        self.regions = self._regions()

    def _router(self) -> dict[str, str]:
        for _section, header, rows in tables(self.text["router"]):
            if header[:2] == ["Provider", "Model"]:
                return {clean(r[1]).lower(): clean(r[2]) for r in rows}  # first table = latest version
        raise SystemExit("Model Router supported-models table not found")

    def _retirements(self) -> dict[str, list[tuple[str, str, str]]]:
        out: dict[str, list] = defaultdict(list)
        for _, header, rows in tables(self.text["retirements"]):
            if header[:3] == ["Model", "Version", "Lifecycle"]:
                for r in rows:
                    out[clean(r[0]).lower()].append((clean(r[1]), clean(r[2]), clean(r[3])))
        return out

    def _regions(self) -> dict[str, dict[str, dict[str, set]]]:
        out: dict = defaultdict(lambda: defaultdict(lambda: defaultdict(set)))
        for key in ("regions_standard", "regions_marketplace"):
            section_type = None
            for section, header, rows in tables(self.text[key]):
                if section in SECTION_TYPES:
                    section_type = SECTION_TYPES[section]
                if not section_type or header[:2] != ["Model", "Version"]:
                    continue
                regions = header[2:]
                for r in rows:
                    name, version = clean(r[0]).lower(), clean(r[1])
                    for region, mark in zip(regions, r[2:], strict=False):
                        if "✅" in mark:
                            out[name][section_type][version].add(region)
        return out

    def lifecycle(self, name: str, version: str) -> tuple[str | None, str | None]:
        entries = self.retirements.get(name.lower(), [])
        exact = [e for e in entries if e[0] == version] or [e for e in entries if e[0] in ("—", "-", "")]
        pick = exact[0] if exact else (entries[0] if len(entries) == 1 else None)
        if not pick:
            return None, None
        lifecycle = pick[1].lower().replace("generally available", "ga")
        retires = pick[2] if re.match(r"\d{4}-\d{2}-\d{2}", pick[2]) else None
        return lifecycle, retires

    def regions_for(self, name: str, version: str) -> dict[str, list[str]]:
        """Regions per deployment type. When the matrix lists this version anywhere, only rows for
        this version count (Claude version 1 has no Data Zone rows); otherwise the latest row is used."""
        found = self.regions.get(name.lower(), {})
        listed = set().union(*found.values()) if found else set()
        result = {}
        for deployment_type, by_version in found.items():
            if version in listed:
                if version in by_version:
                    result[deployment_type] = sorted(by_version[version])
            else:
                result[deployment_type] = sorted(by_version[sorted(by_version)[-1]])
        return result

    def row(self, key: str, name: str) -> list[str]:
        for _, _header, rows in tables(self.text[key]):
            for r in rows:
                if r and first_name(r[0]).lower() == name.lower():
                    return r
        raise SystemExit(f"{name}: not found in {FILES[key]}")


def caps(**values: str) -> dict[str, str]:
    result = {c: UNKNOWN for c in CAPABILITIES}
    result.update(values)
    return result


def parse_openai(row: list[str]) -> dict:
    desc = row[1].lower()
    tools_responses_only = "tool calling (responses api only)" in desc
    has_tools = "functions, tools" in desc or "function calling" in desc
    capabilities = caps(
        reasoning="yes" if "reasoning" in desc else UNKNOWN,
        structured_output="yes" if "structured outputs" in desc else UNKNOWN,
        tools="no" if tools_responses_only else ("yes" if has_tools else UNKNOWN),
        parallel_tools="no" if tools_responses_only else ("yes" if "parallel tool calling" in desc else UNKNOWN),
        streaming="yes" if "streaming" in desc else UNKNOWN,
        image_input="yes" if "image" in desc else ("no" if "text in/text out only" in desc else UNKNOWN),
        computer_use="yes" if "computer use" in desc else UNKNOWN,
    )
    input_match = re.search(r"input:\s*([\d,]+)", row[2], re.I)
    limits = {"context_tokens": number(row[2]), "max_input_tokens": number(input_match.group(1)) if input_match else None,
              "max_output_tokens": number(row[3])}
    return {"capabilities": capabilities, "limits": limits,
            "modalities": {"input": ["text", "image"] if capabilities["image_input"] == "yes" else ["text"], "output": ["text"]},
            "chat_completions": "chat completions api" in desc}


def parse_catalog_row(row: list[str]) -> dict:
    kind, text = row[1].lower(), row[2]
    lowered = text.lower()
    input_match = re.search(r"\*\*input:\*\*\s*([^<]*)", text, re.I)
    output_match = re.search(r"\*\*output:\*\*\s*([^<]*)", text, re.I)
    context_match = re.search(r"\*\*context (?:window|length):\*\*\s*([^<]*)", text, re.I)
    tools_match = re.search(r"\*\*tool calling:\*\*\s*(yes|no)", text, re.I)
    formats_match = re.search(r"\*\*response formats:\*\*\s*([^<|]*)", text, re.I)
    input_text = (input_match.group(1) if input_match else "").lower()
    image = "image" in input_text
    tools = tools_match.group(1).lower() if tools_match else UNKNOWN
    capabilities = caps(
        tools=tools,
        reasoning="yes" if "reasoning" in kind else UNKNOWN,
        image_input="yes" if image else "no",
        json_object="yes" if formats_match and "json" in formats_match.group(1).lower() else UNKNOWN,
        streaming="yes" if "stream" in lowered else UNKNOWN,
    )
    context = number(context_match.group(1)) if context_match else (number(input_text) if input_text else None)
    return {"capabilities": capabilities,
            "limits": {"context_tokens": context, "max_output_tokens": number(output_match.group(1)) if output_match else None},
            "modalities": {"input": ["text", "image"] if image else ["text"], "output": ["text"]},
            "lifecycle_hint": "preview" if "preview" in row[0].lower() else None}


CLAUDE_HOSTING = {"1": ("anthropic", "hosted on anthropic infrastructure"), "2": ("azure", "hosted on azure")}


def parse_claude(row: list[str], doc_version: str) -> dict:
    """Foundry numbers Claude versions by hosting: version 1 runs on Anthropic infrastructure,
    version 2 on Azure. The row's availability column says which versions exist."""
    if doc_version not in CLAUDE_HOSTING:
        raise SystemExit(f"{first_name(row[0])}: Claude doc_version must be 1 (Anthropic-hosted) or 2 (Azure-hosted)")
    hosting, label = CLAUDE_HOSTING[doc_version]
    availability, limits = row[1].lower(), row[2]
    features = (row[3] + " " + (row[4] if len(row) > 4 else "")).lower()  # key capabilities + best for
    stage = re.search(re.escape(label) + r":\s*([a-z ]+)", availability)
    if not stage:
        raise SystemExit(f"{first_name(row[0])}: version {doc_version} ({label}) is not offered in Foundry")
    context_text, _, output_text = limits.partition("/")
    image = "image" in features or "vision" in features
    return {
        # structured_output: the adapter maps response_format=json_schema to output_config.format;
        # json_object has no Messages API equivalent and is not translated.
        "capabilities": caps(tools="yes", parallel_tools="yes", streaming="yes", structured_output="yes",
                             json_object="no",
                             reasoning="yes" if "thinking" in features else UNKNOWN,
                             image_input="yes" if image else UNKNOWN,
                             computer_use="yes" if "computer use" in features else UNKNOWN),
        "limits": {"context_tokens": number(context_text), "max_input_tokens": None,
                   "max_output_tokens": number(output_text)},
        "modalities": {"input": ["text", "image"] if image else ["text"], "output": ["text"]},
        "hosting": hosting,
        "lifecycle_hint": "preview" if "preview" in stage.group(1) else "ga",
    }


ENDPOINTS = {"openai_chat": "/openai/v1/chat/completions", "mai_chat": "/mai/v1/chat/completions",
             "anthropic_messages": "/anthropic/v1/messages"}


def build_entry(spec: dict, docs: Docs, sources: dict, tier: int) -> dict:
    name, version = spec["name"], spec["version"]
    doc_name, doc_version = spec.get("doc_name", name), spec.get("doc_version", version)
    used = []
    parsed: dict = {}
    if spec["docs"] == "openai":
        parsed = parse_openai(docs.row("openai", doc_name))
        used.append(FILES["openai"])
        parsed["capabilities"]["streaming"] = "yes"  # see sources.openai_streaming
        used.append("spec:sources.openai_streaming")
    elif spec["docs"] in ("others", "partners"):
        parsed = parse_catalog_row(docs.row(spec["docs"], doc_name))
        used.append(FILES[spec["docs"]])
    elif spec["docs"] == "claude":
        parsed = parse_claude(docs.row("claude", doc_name), doc_version)
        used += [FILES["claude"], "spec:sources.anthropic_adapter"]
    overrides = spec.get("overrides", {})
    if overrides:
        used.append(f"spec:sources.{overrides['source']}")
    capabilities = {**caps(), **parsed.get("capabilities", {}), **overrides.get("capabilities", {})}
    limits = {"context_tokens": None, "max_input_tokens": None, "max_output_tokens": None,
              **parsed.get("limits", {}), **overrides.get("limits", {})}
    modalities = {"input": ["text"], "output": ["text"], **parsed.get("modalities", {}), **overrides.get("modalities", {})}

    lifecycle, retires = docs.lifecycle(name, version)
    if not lifecycle and (doc_name, doc_version) != (name, version):
        lifecycle, retires = docs.lifecycle(doc_name, doc_version)
    if lifecycle:
        used.append(FILES["retirements"])
    lifecycle = lifecycle or overrides.get("lifecycle") or spec.get("lifecycle") or parsed.get("lifecycle_hint") or UNKNOWN

    regions = docs.regions_for(doc_name, doc_version) or docs.regions_for(name, version)
    if regions:
        used.append(FILES["regions_marketplace" if spec["docs"] == "claude" else "regions_standard"])
    region_status = "published" if regions else "unknown"
    deployment_types = sorted(regions)
    if spec.get("region_scope") == "global":
        region_status, deployment_types = "global", ["global_standard"]
        used.append("spec:sources.fireworks_global")
    hosting = spec.get("hosting") or parsed.get("hosting") or "azure"
    if spec["docs"] == "claude":
        if hosting != "azure":
            regions.pop("data_zone_standard", None)  # Data Zone Standard is offered for Azure-hosted Claude only
        if "global_standard" not in regions:
            # The Claude page says every Claude model, in either hosting, deploys as Global Standard; the
            # region matrix lists some Azure-hosted versions only under Data Zone, so use the model's other row.
            other = docs.regions.get(doc_name.lower(), {}).get("global_standard", {})
            if other:
                regions["global_standard"] = sorted(set().union(*other.values()))
        deployment_types = sorted(regions)
    in_router = name.lower() in docs.router or doc_name.lower() in docs.router

    return {
        "name": name,
        "deployment": spec.get("deployment", name),
        "tier": tier,
        "aliases": spec.get("aliases", []),
        "description": spec["description"],
        "provider": spec["provider"],
        "version": version,
        "lifecycle": lifecycle,
        "retires": retires,
        "in_model_router": in_router,
        "api": spec["api"],
        "endpoint_path": ENDPOINTS[spec["api"]],
        "limits": limits,
        "modalities": modalities,
        "capabilities": capabilities,
        "hosting": {
            "infrastructure": hosting,
            "azure_region_controls_processing": hosting == "azure",
        },
        "deployment_types": deployment_types,
        "regions": regions,
        "region_status": region_status,
        "source": {"docs_commit": docs.commit, "files": sorted(set(used))},
    }


def tiers(names: list[str], pricing: dict) -> dict[str, int]:
    """1 = cheapest, by input price, then output price, then cached-input price, then name."""
    missing = [n for n in names if not (pricing.get(n) or {}).get("input_per_million")]
    if missing:
        raise SystemExit(f"model_pricing.json has no price for {missing}; tiers are computed from prices")

    def key(name: str) -> tuple:
        p = pricing[name]
        return (p["input_per_million"], p.get("output_per_million") or 0,
                p.get("cached_input_per_million") or p["input_per_million"], name.lower())
    return {name: i + 1 for i, name in enumerate(sorted(names, key=key))}


def months_before(day: str, months: int) -> date:
    d = date.fromisoformat(day)
    year, month = divmod(d.month - 1 - months, 12)
    return d.replace(year=d.year + year, month=month + 1, day=min(d.day, 28))


def check_lifecycle_policy(models: list[dict], policy: dict) -> None:
    """Standard-lifecycle models are Deprecated 12 months after launch (6 months before their
    18-month retirement) even when the retirement table still says GA. Fail if one is retained."""
    as_of = date.fromisoformat(policy["as_of"])
    stale = [f"{m['name']} {m['version']} (deprecated since {months_before(m['retires'], policy['deprecated_months_before_retirement'])})"
             for m in models
             if m["provider"] in policy["providers"] and m["retires"] and re.match(r"\d{4}-\d{2}-\d{2}$", m["version"])
             and months_before(m["retires"], policy["deprecated_months_before_retirement"]) <= as_of]
    if stale:
        raise SystemExit(f"deprecated under the lifecycle policy as of {as_of}; move to `excluded`: {stale}")


def table(catalog: dict, pricing: dict) -> str:
    """Markdown summary for docs/MODEL_CATALOGS.md."""
    def size(n):
        if not n:
            return "–"
        if n >= 1_000_000:
            return f"{n / 1_000_000:.3g}M" if n % 1_048_576 else f"{n // 1_048_576}M"
        return f"{n // 1024}K" if n % 1024 == 0 else f"{round(n / 1000)}K"
    lines = ["| Tier | Model | Provider | Version | Lifecycle | Retires | Inference on | Context | In/out $ per 1M | "
             "Tools | Images | JSON schema |", "|" + "---|" * 12]
    for m in sorted(catalog["models"], key=lambda e: e["tier"]):
        p, c = pricing[m["name"]], m["capabilities"]
        lines.append(f"| {m['tier']} | `{m['name']}` | {m['provider']} | {m['version']} | {m['lifecycle']} | "
                     f"{m['retires'] or '–'} | {m['hosting']['infrastructure']} | {size(m['limits']['context_tokens'])} | "
                     f"{p['input_per_million']:g} / {p['output_per_million']:g} | {c['tools']} | {c['image_input']} | "
                     f"{c['structured_output']} |")
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--docs", type=Path, required=True, help="checkout of MicrosoftDocs/azure-ai-docs")
    parser.add_argument("--spec", type=Path, default=ROOT / "config/catalog_spec.json")
    parser.add_argument("--pricing", type=Path, default=ROOT / "config/model_pricing.json")
    parser.add_argument("--out", type=Path, default=ROOT / "config")
    parser.add_argument("--tables", action="store_true", help="also print markdown tables for docs/MODEL_CATALOGS.md")
    args = parser.parse_args()

    spec = json.loads(args.spec.read_text())
    price_file = json.loads(args.pricing.read_text())
    pricing = price_file["models"]
    docs = Docs(args.docs)
    if docs.commit and docs.commit != spec["docs"]["commit"]:
        print(f"warning: docs checkout is {docs.commit}, spec pins {spec['docs']['commit']}", file=sys.stderr)
    for compatibility, catalog in spec["catalogs"].items():
        tier = tiers([m["name"] for m in catalog["models"]], pricing)
        models = sorted((build_entry(m, docs, spec["sources"], tier[m["name"]]) for m in catalog["models"]),
                        key=lambda e: e["tier"])
        check_lifecycle_policy(models, spec["lifecycle_policy"])
        if compatibility == "old":
            missing = set(docs.router) - {m["name"].lower() for m in models} - {e["name"].lower() for e in catalog["excluded"]}
            if missing:
                raise SystemExit(f"old catalog is missing Model Router models: {sorted(missing)}")
        out = {
            "schema_version": 2,
            "compatibility": compatibility,
            "_note": f"{catalog['_note']} {len(models)} models. `tier` is computed from model_pricing.json "
                     "(1 = cheapest). Descriptions are what Decision-1 reads to tell the options apart.",
            "tier_status": f"computed from model_pricing.json version {price_file['version']}: ordered by USD input "
                           "price, then output, cached-input price and name; 1 = cheapest",
            "generated_from": {"spec": "config/catalog_spec.json", "docs": spec["docs"]["repository"],
                               "docs_commit": docs.commit},
            "router_version": catalog.get("router_version"),
            "default_mode": spec["default_mode"],
            "models": models,
            "routing_modes": spec["routing_modes"],
            "excluded_models": catalog.get("excluded", []),
            "sources": spec["sources"],
        }
        path = args.out / f"catalog_{compatibility}.json"
        path.write_text(json.dumps(out, indent=2, ensure_ascii=False) + "\n")
        print(f"wrote {path} ({len(models)} models)")
        if args.tables:
            print(f"\n### `{compatibility}` ({len(models)} models)\n\n{table(out, pricing)}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
