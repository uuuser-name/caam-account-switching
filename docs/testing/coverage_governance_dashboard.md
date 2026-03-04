# Coverage Governance Dashboard

Generated: 2026-03-04T14:42:10Z

| Package | Tier | Coverage | Floor | Target | Stretch | Status | Owner |
|---|---|---:|---:|---:|---:|---|---|
| `./cmd/caam/cmd` | A | 44.4% | 80% | 90% | 95% | regression | cli-team |
| `./internal/exec` | A | 82.8% | 80% | 90% | 95% | on_track | runtime-team |
| `./internal/coordinator` | A | 94.0% | 80% | 90% | 95% | above_target | coordinator-team |
| `./internal/agent` | A | 71.7% | 80% | 90% | 95% | regression | agent-team |
| `./internal/deploy` | B | 72.8% | 70% | 80% | 90% | on_track | deploy-team |
| `./internal/sync` | B | 71.8% | 70% | 80% | 90% | on_track | sync-team |
| `./internal/setup` | B | 83.1% | 70% | 80% | 90% | above_target | setup-team |
| `./internal/provider/claude` | B | 72.9% | 70% | 80% | 90% | on_track | provider-team |
| `./internal/provider/codex` | B | 78.1% | 70% | 80% | 90% | on_track | provider-team |
| `./internal/provider/gemini` | B | 73.8% | 70% | 80% | 90% | on_track | provider-team |
| `./internal/tailscale` | C | 90.5% | 60% | 70% | 80% | above_target | infra-team |

## Hotspots
- `./cmd/caam/cmd` below floor by 35.6% (owner: cli-team)
- `./internal/agent` below floor by 8.299999999999997% (owner: agent-team)
