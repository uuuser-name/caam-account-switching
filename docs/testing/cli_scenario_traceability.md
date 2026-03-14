# CLI Scenario Traceability Map

Generated: 2026-03-14T12:31:15Z
Source bead: bd-3fy.3.5.1

## Summary
- Required scenarios: 227
- Covered: 74 (74 explicit bindings, 0 heuristic matches)
- Uncovered: 153
- Machine-readable map: artifacts/cli-matrix/scenario_traceability.json
- Explicit bindings: artifacts/cli-matrix/scenario_test_bindings.json

## Uncovered Scenarios (Top 50)

| Family | Type | Required Scenario | Suggested Owner |
|---|---|---|---|
| `profile_isolation` | `happy` | `login_isolated_profile` | `unassigned` |
| `profile_isolation` | `failure` | `profile_add_already_exists` | `unassigned` |
| `profile_isolation` | `failure` | `profile_delete_nonexistent` | `unassigned` |
| `profile_isolation` | `failure` | `profile_delete_in_use` | `unassigned` |
| `profile_isolation` | `failure` | `login_browser_failure` | `unassigned` |
| `profile_isolation` | `failure` | `exec_missing_profile` | `unassigned` |
| `profile_isolation` | `edge` | `profile_add_max_profiles` | `unassigned` |
| `profile_isolation` | `edge` | `exec_concurrent_sessions` | `unassigned` |
| `profile_isolation` | `edge` | `login_interrupted_flow` | `unassigned` |
| `profile_isolation` | `edge` | `exec_with_invalid_args` | `unassigned` |
| `rotation_cooldown` | `failure` | `activate_auto_no_profiles` | `unassigned` |
| `rotation_cooldown` | `failure` | `run_command_failure` | `unassigned` |
| `rotation_cooldown` | `failure` | `run_max_retries_exceeded` | `unassigned` |
| `rotation_cooldown` | `edge` | `activate_auto_single_profile` | `unassigned` |
| `rotation_cooldown` | `edge` | `run_concurrent_executions` | `unassigned` |
| `rotation_cooldown` | `edge` | `cooldown_set_expired_time` | `unassigned` |
| `project_association` | `happy` | `project_clear_all` | `unassigned` |
| `project_association` | `failure` | `project_set_invalid_profile` | `unassigned` |
| `project_association` | `failure` | `project_get_no_association` | `unassigned` |
| `project_association` | `failure` | `project_remove_nonexistent` | `unassigned` |
| `project_association` | `edge` | `project_set_nested_directory` | `unassigned` |
| `project_association` | `edge` | `project_set_overwrites_existing` | `unassigned` |
| `project_association` | `edge` | `project_get_cascades_from_parent` | `unassigned` |
| `monitoring_health` | `happy` | `monitor_shows_all_tools` | `unassigned` |
| `monitoring_health` | `happy` | `doctor_all_healthy` | `unassigned` |
| `monitoring_health` | `happy` | `validate_auth_files` | `unassigned` |
| `monitoring_health` | `happy` | `verify_profile_integrity` | `unassigned` |
| `monitoring_health` | `failure` | `monitor_no_auth` | `unassigned` |
| `monitoring_health` | `failure` | `precheck_expired_token` | `unassigned` |
| `monitoring_health` | `failure` | `precheck_invalid_auth` | `unassigned` |
| `monitoring_health` | `failure` | `doctor_finds_issues` | `unassigned` |
| `monitoring_health` | `failure` | `validate_missing_files` | `unassigned` |
| `monitoring_health` | `failure` | `verify_corrupted_profile` | `unassigned` |
| `monitoring_health` | `edge` | `monitor_multiple_providers` | `unassigned` |
| `monitoring_health` | `edge` | `doctor_quiet_mode` | `unassigned` |
| `session_history` | `happy` | `sessions_list_active` | `unassigned` |
| `session_history` | `happy` | `history_shows_switches` | `unassigned` |
| `session_history` | `happy` | `usage_shows_totals` | `unassigned` |
| `session_history` | `happy` | `cost_shows_estimates` | `unassigned` |
| `session_history` | `happy` | `cost_tokens_breakdown` | `unassigned` |
| `session_history` | `failure` | `sessions_no_data` | `unassigned` |
| `session_history` | `failure` | `history_empty` | `unassigned` |
| `session_history` | `failure` | `usage_no_activity` | `unassigned` |
| `session_history` | `failure` | `cost_no_rates_configured` | `unassigned` |
| `session_history` | `edge` | `sessions_long_running` | `unassigned` |
| `session_history` | `edge` | `history_large_dataset` | `unassigned` |
| `session_history` | `edge` | `usage_filter_by_date` | `unassigned` |
| `session_history` | `edge` | `cost_custom_rates` | `unassigned` |
| `sync` | `happy` | `sync_add_machine` | `unassigned` |
| `sync` | `happy` | `sync_enable_syncing` | `unassigned` |

## Notes
- Explicit bindings are declared in `artifacts/cli-matrix/scenario_test_bindings.json`.
- Heuristic matching uses normalized string similarity between required scenario names and existing test scenario IDs.
- Exact scenario-to-test bindings should be refined as C3.2 fills matrix deficits.
