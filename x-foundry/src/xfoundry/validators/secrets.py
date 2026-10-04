"""Rule 17: raw secret values are rejected; only Key Vault references are permitted."""

from __future__ import annotations

import re
from typing import Any

from xfoundry.diagnostics import Diagnostic, error
from xfoundry.parser.schema_validation import format_path

_KV_REFERENCE = re.compile(
    r"^(?:@Microsoft\.KeyVault\(.+\)"
    r"|keyvault:[A-Za-z0-9-]{1,127}"
    r"|https://[A-Za-z0-9-]{3,24}\.vault\.azure\.net/secrets/[A-Za-z0-9-]{1,127}(?:/[0-9a-f]{32})?)$"
)
_SECRET_NAME = re.compile(r"^[A-Za-z0-9-]{1,127}$")
_SENSITIVE_KEY = re.compile(
    r"(?i)(api[-_]?key|secret|password|passwd|pwd|access[-_]?token|auth[-_]?token|bearer"
    r"|connection[-_]?string|sas[-_]?token|private[-_]?key|client[-_]?secret|credential"
    r"|subscription[-_]?key|(^|[-_])token$|^authorization$|^cookie$)"
)
_RAW_SECRET_PATTERNS = (
    (
        re.compile(r"(?i)(AccountKey|SharedAccessKey|SharedAccessSignature)\s*="),
        "a storage or bus key",
    ),
    (re.compile(r"(?i)[?&]sig=[A-Za-z0-9%+/=]{20,}"), "a SAS signature"),
    (re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----"), "a private key"),
    (re.compile(r"\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}"), "a JWT"),
    (re.compile(r"\bsk-[A-Za-z0-9_-]{20,}"), "an API key"),
    (re.compile(r"\bgh[pousr]_[A-Za-z0-9]{30,}"), "a GitHub token"),
    (re.compile(r"(?i)\bpassword\s*=\s*\S+"), "a password"),
    (re.compile(r"://[^/\s:@]+:[^/\s@]+@"), "credentials embedded in a URL"),
)


def is_key_vault_reference(value: str) -> bool:
    return bool(_KV_REFERENCE.match(value))


def is_secret_name_or_reference(value: str) -> bool:
    return is_key_vault_reference(value) or bool(_SECRET_NAME.match(value))


def raw_secret_kind(value: str) -> str | None:
    for pattern, kind in _RAW_SECRET_PATTERNS:
        if pattern.search(value):
            return kind
    return None


def _scan(node: Any, path: list[str | int], out: list[Diagnostic]) -> None:
    if isinstance(node, dict):
        for key, value in node.items():
            child = [*path, str(key)]
            sensitive_slot = (
                path
                and path[-1] in {"environment", "headers"}
                and isinstance(value, str)
                and _SENSITIVE_KEY.search(str(key))
            )
            if sensitive_slot and not is_key_vault_reference(value):
                out.append(
                    error(
                        "XF017",
                        f"'{key}' looks like a secret; use a Key Vault reference "
                        "(@Microsoft.KeyVault(...), keyvault:<name> or a vault secret URI)",
                        format_path(child),
                    )
                )
            else:
                _scan(value, child, out)
    elif isinstance(node, list):
        for index, item in enumerate(node):
            _scan(item, [*path, index], out)
    elif isinstance(node, str):
        kind = raw_secret_kind(node)
        if kind:
            out.append(
                error(
                    "XF017",
                    f"value appears to contain {kind}; store it in Key Vault and reference it",
                    format_path(path),
                )
            )


def find_raw_secrets(dumped: dict[str, Any]) -> list[Diagnostic]:
    """Scan the configuration (as a camelCase mapping) for raw secret material."""
    out: list[Diagnostic] = []
    _scan(dumped, [], out)
    return out
