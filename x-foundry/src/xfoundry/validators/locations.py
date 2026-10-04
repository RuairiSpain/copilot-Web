"""Rule 23: Azure location checks."""

from __future__ import annotations

KNOWN_REGIONS = frozenset(
    [
        "australiacentral",
        "australiaeast",
        "australiasoutheast",
        "austriaeast",
        "belgiumcentral",
        "brazilsouth",
        "brazilsoutheast",
        "canadacentral",
        "canadaeast",
        "centralindia",
        "centralus",
        "chilecentral",
        "eastasia",
        "eastus",
        "eastus2",
        "francecentral",
        "germanywestcentral",
        "indonesiacentral",
        "israelcentral",
        "italynorth",
        "japaneast",
        "japanwest",
        "koreacentral",
        "koreasouth",
        "malaysiawest",
        "mexicocentral",
        "newzealandnorth",
        "northcentralus",
        "northeurope",
        "norwayeast",
        "polandcentral",
        "qatarcentral",
        "southafricanorth",
        "southcentralus",
        "southindia",
        "southeastasia",
        "spaincentral",
        "swedencentral",
        "switzerlandnorth",
        "switzerlandwest",
        "uaenorth",
        "uksouth",
        "ukwest",
        "westcentralus",
        "westeurope",
        "westindia",
        "westus",
        "westus2",
        "westus3",
    ]
)


def canonical(location: str) -> str:
    """``West Europe`` and ``westeurope`` name the same region."""
    return "".join(location.split()).lower()


def is_known_region(location: str) -> bool:
    return canonical(location) in KNOWN_REGIONS
