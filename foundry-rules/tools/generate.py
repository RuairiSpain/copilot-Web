#!/usr/bin/env python3
"""Maintainer tool. Regenerates the committed files of foundry-rules from the catalogue.

Users of the rules never run this. It is not a dependency of the rule set.
It reads foundry-doctor/rules/catalog/**/FND-*.yaml (read only) and needs PyYAML to do so.

Writes:
  .ps-rule/Foundry.Rule.yaml, .ps-rule/Foundry.Rule.ps1, .ps-rule/en/FND-*.md
  baselines/{dev,test,prod}.Rule.yaml
  docs/rule-index.md, docs/not-evaluated.md

Usage: python3 foundry-rules/tools/generate.py [catalogue-dir]
"""
import glob
import os
import re
import sys

import yaml

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from rulelogic import PS, YAML  # noqa: E402
from rulespec import CLASS_NAME, CLS, INPUT, LIVE, SUBSET  # noqa: E402

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DEFAULT_CATALOG = os.path.join(os.path.dirname(ROOT), "foundry-doctor", "rules", "catalog")
LEVEL = {"error": "Error", "warning": "Warning", "info": "Information"}
API = "github.com/microsoft/PSRule/v1"
UNVERIFIED = re.compile(r"[Uu]nverified|not (independently )?verified|was not (searched|read|confirmed)")


def flat(text):
    return " ".join(str(text).split())


def load(catalog):
    rules = {}
    for path in sorted(glob.glob(os.path.join(catalog, "*", "FND-*.yaml"))):
        with open(path, encoding="utf-8") as fh:
            d = yaml.safe_load(fh)
        rules[d["id"]] = d
    return rules


def dump(doc):
    return yaml.safe_dump(doc, sort_keys=False, default_flow_style=False, width=1000, allow_unicode=True)


def status_of(rid):
    return "evaluated-subset" if CLS[rid] in ("sy", "sp") else "not-evaluated"


def caveat(d):
    notes = flat(d.get("notes", ""))
    return "unverified-notes" if UNVERIFIED.search(notes) else ""


def tags(d):
    rid = d["id"]
    return {
        "ruleSet": "foundry-rules",
        "group": d["group"],
        "evaluation": CLASS_NAME[CLS[rid]],
        "status": status_of(rid),
        "catalogueStatus": d["status"],
    }


def yaml_rule(d):
    rid = d["id"]
    spec = {"level": LEVEL[d["severity"]["prod"]]}
    if CLS[rid] == "sy":
        types, where, cond = YAML[rid]
        spec["type"] = types
        if where:
            spec["where"] = where
        spec["condition"] = cond
    else:
        # No input object has this type, so the rule never runs and never reports Pass.
        spec["type"] = ["Foundry.NotEvaluated"]
        spec["condition"] = {"field": "name", "exists": True}
    doc = {"apiVersion": API, "kind": "Rule", "metadata": {"name": rid, "tags": tags(d)}, "spec": spec}
    src = d["sources"][0]
    head = "# %s source: %s (verified %s)\n" % (rid, src["url"], src["lastVerified"])
    return head + dump(doc)


def ps_rule(d):
    rid = d["id"]
    params, body = PS[rid]
    t = tags(d)
    tag = "; ".join("%s = '%s'" % (k, v) for k, v in t.items())
    lines = ["# %s source: %s (verified %s)" % (rid, d["sources"][0]["url"], d["sources"][0]["lastVerified"]),
             "Rule '%s' -Tag @{ %s } %s {" % (rid, tag, params)]
    lines += ["    " + ln if ln.strip() else ln for ln in body.strip("\n").split("\n")]
    lines.append("}")
    return "\n".join(lines) + "\n"


def doc_md(d):
    rid = d["id"]
    sev = d["severity"]
    cls = CLS[rid]
    out = ["# %s" % rid, "", "## SYNOPSIS", "", d["title"], "", "## DESCRIPTION", "", flat(d["description"]), "",
           "## RECOMMENDATION", "", flat(d["recommendation"]), ""]
    out += ["## EXAMPLES", "", "```", d["fix"].rstrip("\n"), "```", ""]
    out += ["## NOTES", ""]
    out.append("- Severity dev/test/prod: %s/%s/%s (PSRule: %s/%s/%s)." % (
        sev["dev"], sev["test"], sev["prod"], LEVEL[sev["dev"]], LEVEL[sev["test"]], LEVEL[sev["prod"]]))
    out.append("- Catalogue status: %s. Catalogue decision: %s." % (d["status"], d["overlap"]["decision"]))
    out.append("- Evaluation class: %s. Status: %s." % (CLASS_NAME[cls], status_of(rid)))
    if cls in ("sy", "sp"):
        out.append("- Evaluated here: " + SUBSET[rid])
    elif cls == "live":
        out.append("- Not evaluated by PSRule: " + LIVE[rid])
        out.append("- This rule never reports Pass in PSRule. Use Foundry Doctor with live access.")
    else:
        out.append("- Not evaluated by PSRule: " + INPUT[rid][0])
        out.append("- How to feed it: " + INPUT[rid][1])
        out.append("- This rule never reports Pass in PSRule.")
    if d["status"] == "product-opinion":
        out.append("- Product opinion: the behaviour is not documented as an Azure or Foundry platform fact.")
    if caveat(d):
        out.append("- The catalogue notes contain unverified items; see the catalogue entry notes field.")
    m = d["overlap"].get("research", {}).get("psrule", {})
    if m.get("matches"):
        out.append("- Related PSRule for Azure rules from the catalogue research: " + ", ".join(m["matches"]) + ".")
    out += ["", "## LINKS", ""]
    for s in d["sources"]:
        out.append("- [%s](%s) (last verified %s)" % (s["url"], s["url"], s["lastVerified"]))
    return "\n".join(out) + "\n"


def baseline(profile, rules):
    levels = {rid: LEVEL[d["severity"][profile]] for rid, d in sorted(rules.items())}
    doc = {"apiVersion": API, "kind": "Baseline", "metadata": {"name": "Foundry." + profile.capitalize()},
           "spec": {"rule": {"tag": {"ruleSet": "foundry-rules"}}, "override": {"level": levels}}}
    return dump(doc)


def table(rows):
    return "\n".join(rows) + "\n"


def index_md(rules):
    rows = ["| ID | Title | dev | test | prod | How evaluated | Status | Catalogue status |", "|---|---|---|---|---|---|---|---|"]
    for rid, d in sorted(rules.items()):
        s = d["severity"]
        cat = d["status"] + (", unverified notes" if caveat(d) else "")
        rows.append("| %s | %s | %s | %s | %s | %s | %s | %s |" % (
            rid, d["title"].replace("|", "\\|"), s["dev"], s["test"], s["prod"], CLASS_NAME[CLS[rid]], status_of(rid), cat))
    counts = {}
    for rid in rules:
        counts[CLASS_NAME[CLS[rid]]] = counts.get(CLASS_NAME[CLS[rid]], 0) + 1
    head = ["<!-- Generated by tools/generate.py. Do not edit by hand. -->", "", "# Foundry rule index", "",
            "%d rules, same IDs, titles and severities as the Foundry Doctor catalogue." % len(rules), "",
            "Severity values are the catalogue values (error, warning, info). Rule files carry the prod value as the "
            "PSRule level; the baselines in `baselines/` override it per profile. Mapping: error = Error, warning = "
            "Warning, info = Information.", "",
            "Status `evaluated-subset`: PSRule evaluates the subset of the catalogue evidence listed on the rule page. "
            "Status `not-evaluated`: the rule exists but never runs and never reports Pass; see `not-evaluated.md`.", "",
            "| How evaluated | Rules |", "|---|---|"]
    head += ["| %s | %d |" % (k, counts[k]) for k in ("static-yaml", "static-ps", "needs-live", "needs-input")]
    head.append("")
    return "\n".join(head) + "\n" + table(rows)


def not_eval_md(rules):
    rows = ["| ID | Title | Class | Reason | How to feed it |", "|---|---|---|---|---|"]
    for rid, d in sorted(rules.items()):
        c = CLS[rid]
        if c == "live":
            rows.append("| %s | %s | needs-live | %s | Run Foundry Doctor with live access. |" % (rid, d["title"], LIVE[rid]))
        elif c == "inp":
            rows.append("| %s | %s | needs-input | %s | %s |" % (rid, d["title"], INPUT[rid][0], INPUT[rid][1]))
    head = ["<!-- Generated by tools/generate.py. Do not edit by hand. -->", "", "# Rules that are not evaluated", "",
            "These rules are defined in `.ps-rule/Foundry.Rule.yaml` with a type that no input object has "
            "(`Foundry.NotEvaluated`). They never run, so PSRule never reports them as passed.", "",
            "- needs-live: the evidence comes from the Azure control plane, a data plane, a runtime probe or a cost API.",
            "- needs-input: the evidence comes from input PSRule does not receive as one object: `.env` files, "
            "pipeline files, API Management policy XML, JSON schemas, profile declarations, or several resources that must "
            "be correlated. A `resource-set wrapper` means one JSON object that holds all resources of a template; it is "
            "not implemented in this version.", ""]
    return "\n".join(head) + "\n" + table(rows)


def main():
    catalog = sys.argv[1] if len(sys.argv) > 1 else DEFAULT_CATALOG
    rules = load(catalog)
    assert len(rules) == 108, len(rules)
    missing = set(rules) - set(CLS)
    assert not missing, missing
    ps_ids = sorted(r for r in rules if CLS[r] == "sp")
    assert sorted(PS) == ps_ids
    assert sorted(YAML) == sorted(r for r in rules if CLS[r] == "sy")

    def w(rel, text):
        path = os.path.join(ROOT, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8", newline="\n") as fh:
            fh.write(text)

    ymls = [yaml_rule(d) for rid, d in sorted(rules.items()) if CLS[rid] != "sp"]
    w(".ps-rule/Foundry.Rule.yaml",
      "# Generated by tools/generate.py from the Foundry Doctor catalogue. Do not edit by hand.\n---\n"
      + "---\n".join(ymls))
    pss = [ps_rule(rules[r]) for r in ps_ids]
    w(".ps-rule/Foundry.Rule.ps1",
      "# Generated by tools/generate.py from the Foundry Doctor catalogue. Do not edit by hand.\n"
      "# Static PowerShell rules. Parse-checked only; not yet run by PSRule (see README.md, Status).\n\n" + "\n".join(pss))
    for rid, d in rules.items():
        w(".ps-rule/en/%s.md" % rid, doc_md(d))
    for p in ("dev", "test", "prod"):
        w("baselines/%s.Rule.yaml" % p, "# Generated by tools/generate.py. Do not edit by hand.\n" + baseline(p, rules))
    w("docs/rule-index.md", index_md(rules))
    w("docs/not-evaluated.md", not_eval_md(rules))
    print("generated %d rules" % len(rules))


if __name__ == "__main__":
    main()
