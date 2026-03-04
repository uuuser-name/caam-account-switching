# Critical Uncovered Inventory

This inventory is generated from risk-tier policy and latest package coverage scan.

## Inventory Fields
- Package
- Tier
- Coverage
- Floor
- Owner
- Gap to Floor
- Follow-up

## Current Hotspots
| Package | Tier | Coverage | Floor | Owner | Gap to Floor | Follow-up |
|---|---|---:|---:|---|---:|---|
| `./cmd/caam/cmd` | A | 30.2% | 80% | cli-team | 49.8% | `bd-1r67.2.3` |

## Near-Risk Packages
| Package | Tier | Coverage | Floor | Owner | Margin | Follow-up |
|---|---|---:|---:|---|---:|---|
| `./internal/agent` | A | 71.6% | 80% | agent-team | -8.4% | `bd-1r67.2.4.4` verification refresh |

## Healthy Packages
| Package | Tier | Coverage | Floor | Owner |
|---|---|---:|---:|---|
| `./internal/exec` | A | 80.8% | 80% | runtime-team |
| `./internal/coordinator` | A | 94.0% | 80% | coordinator-team |
| `./internal/sync` | B | 71.8% | 70% | sync-team |
| `./internal/provider/claude` | B | 72.9% | 70% | provider-team |
| `./internal/provider/codex` | B | 78.1% | 70% | provider-team |
| `./internal/provider/gemini` | B | 73.8% | 70% | provider-team |

## Owner Mapping Rules
- `cli-team`: command surface and user interaction flows.
- `runtime-team`: command execution and orchestration runtime.
- `coordinator-team`: auth-recovery coordination and backend control.
- `agent-team`: local API and agent process behavior.
- `provider-team`: provider adapters and schema-drift compatibility.
- `sync-team`: remote sync and SSH behavior.
- `deploy-team`: remote install/update deployment lane.
- `setup-team`: bootstrap and environment setup.
- `infra-team`: network and tailscale integration.

## Automation
For a refreshed machine-generated version, run:

```bash
./scripts/build_coverage_governance_dashboard.sh
```

That command updates both dashboard artifacts and hotspot status.
