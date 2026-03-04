# Provider Parity + Schema Drift Matrix

Updated: 2026-03-02

## Coverage Floor Status
| Package | Coverage | Floor | Status |
|---|---:|---:|---|
| internal/provider/claude | 72.9% | 70% | PASS |
| internal/provider/codex | 78.1% | 70% | PASS |
| internal/provider/gemini | 73.8% | 70% | PASS |

## Shared Fixture Corpus
| Fixture | Purpose | Consumed by test |
|---|---|---|
| `internal/provider/testdata/schema_drift/claude_credentials_valid.json` | Claude modern OAuth payload with millis expiry | `TestSharedFixtureCorpusClaude` |
| `internal/provider/testdata/schema_drift/codex_auth_valid.json` | Codex auth payload with token + RFC3339 expiry | `TestSharedFixtureCorpusCodex` |
| `internal/provider/testdata/schema_drift/gemini_oauth_valid.json` | Gemini OAuth payload with token + refresh + expiry | `TestSharedFixtureCorpusGemini` |

## Behavioral Parity Checks
| Concern | Claude | Codex | Gemini |
|---|---|---|---|
| Passive token validation | yes (`ValidateToken(..., true)`) | yes | yes |
| Active token validation path | yes (method corrected to `active`) | yes (method corrected to `active`) | yes (method corrected to `active`) |
| Expiry parsing (RFC3339 + unix) | yes | yes | yes |
| Older millis timestamp handling | fixed (`>=1e11` heuristic) | fixed (`>=1e11`) | fixed (`>=1e11`) |
| Invalid JSON handling | explicit tests | explicit tests | explicit tests |
| Missing auth/file state handling | explicit tests | explicit tests | explicit tests |

## Known Intentional Differences
| Topic | Claude | Codex | Gemini |
|---|---|---|---|
| API key interactive flow | guidance (`loginWithAPIKey`) | `codex login --with-api-key` command path | prompts + `.env` write |
| OAuth command dependency | `claude` CLI | `codex` CLI | `gemini` CLI |
| Additional auth mode | OAuth + API key | OAuth + Device Code + API key | OAuth + API key + Vertex ADC |

