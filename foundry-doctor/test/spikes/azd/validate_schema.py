#!/usr/bin/env python3
"""Validate an azure.yaml against the azd JSON schema, resolving the extension schema $refs from a local azure-dev clone.

Usage: validate_schema.py <azure-dev clone dir> <azure.yaml>
Needs: pyyaml, jsonschema >= 4.18 (with the `referencing` package).
Exit: 0 valid, 1 schema errors (printed one per line), 2 usage or environment error.

The schema $refs point at https://raw.githubusercontent.com/Azure/azure-dev/main/..., which the clone mirrors, so no
network access is needed. The result is only as current as the clone; record its commit when you cite the result.
"""
import json
import os
import sys

RAW_PREFIX = "https://raw.githubusercontent.com/Azure/azure-dev/main/"


def main(argv):
    if len(argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2
    clone, target = argv[1], argv[2]
    try:
        import yaml
        from jsonschema import Draft7Validator, Draft202012Validator, validators
        from referencing import Registry, Resource
    except ImportError as e:
        print(f"missing dependency: {e}", file=sys.stderr)
        return 2

    def load(path):
        with open(path, encoding="utf-8") as f:
            return json.load(f)

    def retrieve(uri):
        if not uri.startswith(RAW_PREFIX):
            raise LookupError(f"refusing to fetch {uri}")
        local = os.path.join(clone, uri[len(RAW_PREFIX):])
        return Resource.from_contents(load(local))

    schema_path = os.path.join(clone, "schemas", "v1.0", "azure.yaml.json")
    schema = load(schema_path)
    cls = validators.validator_for(schema, default=Draft7Validator)
    validator = cls(schema, registry=Registry(retrieve=retrieve))
    with open(target, encoding="utf-8") as f:
        doc = yaml.safe_load(f)
    errors = sorted(validator.iter_errors(doc), key=lambda e: [str(p) for p in e.absolute_path])
    for e in errors:
        print(f"{'/'.join(str(p) for p in e.absolute_path) or '<root>'}: {e.message}")
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
