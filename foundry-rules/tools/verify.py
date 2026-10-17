#!/usr/bin/env python3
"""Maintainer check. Static consistency of foundry-rules against the catalogue. Needs PyYAML.

Does not run PSRule. Exit code 1 on any failure.
Usage: python3 foundry-rules/tools/verify.py [catalogue-dir]
"""
import glob
import json
import os
import re
import sys

import yaml

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
CATALOG = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(ROOT), "foundry-doctor", "rules", "catalog")
LEVEL = {"error": "Error", "warning": "Warning", "info": "Information"}
errors = []


def fail(msg):
    errors.append(msg)


def read(rel):
    with open(os.path.join(ROOT, rel), encoding="utf-8") as fh:
        return fh.read()


cat = {}
for p in sorted(glob.glob(os.path.join(CATALOG, "*", "FND-*.yaml"))):
    d = yaml.safe_load(open(p, encoding="utf-8"))
    cat[d["id"]] = d
if len(cat) != 108:
    fail("catalogue has %d rules, expected 108" % len(cat))

# 1. YAML and JSON parse
parsed = {}
for rel in [".ps-rule/Foundry.Rule.yaml", "ps-rule.yaml"] + [
        "baselines/%s.Rule.yaml" % p for p in ("dev", "test", "prod")]:
    try:
        parsed[rel] = [x for x in yaml.safe_load_all(read(rel)) if x is not None]
    except Exception as exc:  # noqa: BLE001
        fail("YAML parse error in %s: %s" % (rel, exc))
for f in glob.glob(os.path.join(ROOT, "tests", "*.yaml")):
    try:
        yaml.safe_load(open(f, encoding="utf-8"))
    except Exception as exc:  # noqa: BLE001
        fail("YAML parse error in %s: %s" % (f, exc))
for f in glob.glob(os.path.join(ROOT, "tests", "*.json")):
    try:
        json.load(open(f, encoding="utf-8"))
    except Exception as exc:  # noqa: BLE001
        fail("JSON parse error in %s: %s" % (f, exc))

# 2. Each ID exactly once in rule files, docs, index, baselines
yaml_rules = {}
for doc in parsed.get(".ps-rule/Foundry.Rule.yaml", []):
    n = doc["metadata"]["name"]
    if n in yaml_rules:
        fail("duplicate YAML rule " + n)
    yaml_rules[n] = doc
ps_text = read(".ps-rule/Foundry.Rule.ps1")
ps_ids = re.findall(r"^Rule '(FND-[A-Z]+-\d{3})'", ps_text, re.M)
for i in set(ps_ids):
    if ps_ids.count(i) != 1:
        fail("duplicate PS rule " + i)
for i in cat:
    in_yaml, in_ps = i in yaml_rules, i in ps_ids
    if in_yaml == in_ps:
        fail("%s must be in exactly one of Foundry.Rule.yaml and Foundry.Rule.ps1" % i)
    if not os.path.exists(os.path.join(ROOT, ".ps-rule", "en", i + ".md")):
        fail("missing doc for " + i)
extra = (set(yaml_rules) | set(ps_ids)) - set(cat)
if extra:
    fail("rule files define IDs not in the catalogue: %s" % sorted(extra))

index = read("docs/rule-index.md")
rows = {}
for m in re.finditer(r"^\| (FND-[A-Z]+-\d{3}) \| (.*?) \| (\w+) \| (\w+) \| (\w+) \| ([\w-]+) \| ([\w-]+) \|", index, re.M):
    if m.group(1) in rows:
        fail("duplicate index row " + m.group(1))
    rows[m.group(1)] = m.groups()
if set(rows) != set(cat):
    fail("index IDs differ from catalogue: %s" % sorted(set(rows) ^ set(cat)))
not_eval = read("docs/not-evaluated.md")
for rid, d in cat.items():
    if rid not in rows:
        continue
    _, title, dev, test, prod, how, status = rows[rid]
    s = d["severity"]
    if title.replace("\\|", "|") != d["title"]:
        fail("title differs for " + rid)
    if (dev, test, prod) != (s["dev"], s["test"], s["prod"]):
        fail("severity differs in index for " + rid)
    if status == "not-evaluated" and ("| %s |" % rid) not in not_eval:
        fail("not-evaluated rule missing from not-evaluated.md: " + rid)
    if status != "not-evaluated" and ("| %s |" % rid) in not_eval:
        fail("evaluated rule listed in not-evaluated.md: " + rid)
    if rid in yaml_rules:
        r = yaml_rules[rid]
        if r["spec"]["level"] != LEVEL[s["prod"]]:
            fail("default level is not prod severity for " + rid)
        if (status == "not-evaluated") != (r["spec"]["type"] == ["Foundry.NotEvaluated"]):
            fail("status and rule type disagree for " + rid)
    if status == "not-evaluated" and rid in ps_ids:
        fail("not-evaluated rule has PowerShell body: " + rid)
for prof in ("dev", "test", "prod"):
    docs = parsed.get("baselines/%s.Rule.yaml" % prof, [])
    levels = docs[0]["spec"]["override"]["level"] if docs else {}
    if set(levels) != set(cat):
        fail("baseline %s does not list every rule once" % prof)
    for rid, d in cat.items():
        if levels.get(rid) != LEVEL[d["severity"][prof]]:
            fail("baseline %s level differs for %s" % (prof, rid))

# 3. Property paths used by rules appear in the catalogue evidence of the same rule
SKIP = {"properties", "type", "kind", "name", "sku", "identity", "value", "count", "psobject", "length", "tags",
        "services", "host", "project", "uses", "env", "tools", "connection", "connections", "toolboxes"}


def evidence_text(d):
    parts = [d.get("description", ""), d["evidence"]["description"], d.get("notes", ""), d.get("fix", ""),
             d.get("recommendation", "")]
    return " ".join(str(p) for p in parts).lower()


def fields(node, out):
    if isinstance(node, dict):
        if "field" in node and isinstance(node["field"], str):
            out.append(node["field"])
        for v in node.values():
            fields(v, out)
    elif isinstance(node, list):
        for v in node:
            fields(v, out)


for rid, r in yaml_rules.items():
    out = []
    fields(r["spec"], out)
    text = evidence_text(cat[rid])
    for path in out:
        for seg in path.split("."):
            if seg.lower() not in SKIP and seg.lower() not in text:
                fail("%s: path segment '%s' (%s) not in catalogue evidence" % (rid, seg, path))
for m in re.finditer(r"^Rule '(FND-[A-Z]+-\d{3})'.*?^}", ps_text, re.M | re.S):
    rid, body = m.group(1), m.group(0)
    text = evidence_text(cat[rid])
    segs = set()
    for chain in re.findall(r"\$(?:TargetObject|p|v|t|_)\.([A-Za-z_][\w.]*)", body):
        segs.update(chain.split("."))
    segs.update(re.findall(r"PSObject\.Properties\['(\w+)'\]", body))
    segs.update(re.findall(r"\)\.([A-Za-z_]\w*)", body))
    for seg in sorted(segs):
        if seg.lower() in SKIP or seg.lower() in {"properties", "psobject"}:
            continue
        if seg.lower() not in text:
            fail("%s: PowerShell member '%s' not in catalogue evidence" % (rid, seg))

if errors:
    print("FAILED (%d)" % len(errors))
    for e in errors:
        print(" -", e)
    sys.exit(1)
print("OK: %d rules; yaml rules %d, ps rules %d, docs %d, index rows %d" % (
    len(cat), len(yaml_rules), len(ps_ids), len(glob.glob(os.path.join(ROOT, ".ps-rule", "en", "FND-*.md"))), len(rows)))
