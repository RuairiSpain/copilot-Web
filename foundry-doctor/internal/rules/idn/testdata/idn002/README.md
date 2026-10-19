# FND-IDN-002 compiled fixtures

Each `X.json` is the ARM JSON compiled from the `X.bicep` next to it with Bicep CLI 0.47.16
(`bicep build X.bicep --outfile X.json`). `idn002_test.go` loads them with `armmodel.FromARM`.

| Fixture | Scenario |
| --- | --- |
| `good` | Project with a system-assigned identity linked to Search and Storage, holding every catalogue-listed role. |
| `uai-missing` | Project with a user-assigned identity linked to Search; Search Service Contributor is missing. |

The role GUIDs are the verified built-in role definition IDs from `rules/catalog/idn/FND-IDN-002.yaml`.
The fixtures contain no secrets.
