# Intelligence monitoring

Private administrator APIs under `/api/v1/admin/intelligence-monitors` compare generated HTML artifacts using the fixed `gpt-6-astra` model, `high` reasoning and the fixed Chinese pelican-on-a-bicycle prompt. Plans are paused by default. Generation is asynchronous; new requests for a busy plan are rejected and at most two workers run globally.

## Sources

`external` stores an encrypted API key for a public HTTPS endpoint. `upstream` references an existing upstream target. `local_group` uses a dedicated restricted API key and the local gateway to preserve the selected group's actual routing and billing rules.

`openai_oauth` accepts `account_id` referencing a real OpenAI OAuth account. The plan name and current `source_name` come from the account; supplied names are ignored. This source always uses Responses. Shadow accounts and synthetic UI fixtures are excluded; the account must support the fixed model without remapping. Execution checks current scheduling status, proxy availability and account/proxy concurrency. Invalid or unavailable accounts produce a failed record without trying another account.

The OAuth runner calls `OpenAIGatewayService.Forward` with the selected account. Existing token refresh, bound proxy, TLS and transport behavior are reused. No bearer/refresh token is copied into the monitoring tables or returned in monitoring DTOs. The gateway's internal protocol compatibility handling remains in effect; the monitor never retries a failed generation or enters the account scheduler. Background requests opt into cancellation on task timeout or service shutdown, and a maximum 4 MiB response. Source snapshots retain the selected account ID/name, OAuth marker and actual model/effort when available. OAuth does not pretend to have an upstream group billing multiplier.

## History retention

Each plan retains the latest **20 terminal records** (`succeeded` or `failed`), ordered by `created_at DESC, id DESC`. Older terminal rows, including HTML/raw text, are physically deleted. Active `pending`/`running` rows are excluded. Cleanup runs on service startup and transactionally after completion or expired lease handling, including archived plans.

Plan list responses include `recent_runs` (latest first, up to 20 terminal summaries) and `latest_run` (which can be pending/running). Full HTML/raw text remain detail-only via `/runs/:id`; generated documents must be previewed inside an isolated sandbox. Old record IDs return not found after retention removes them.

## Verification

Unit tests cover immutable prompt/effort, source validation, automatic account names, cancellation, concurrency/proxy failures, response limits, source snapshots and retention scheduling. The OAuth integration test uses the real gateway with a fake HTTP upstream/token cache; it makes no model request. PostgreSQL tests use an opt-in `UPSTREAM_TEST_DATABASE_URL` and a private temporary schema to cover idempotent migrations, CRUD, account deletion, history persistence and physical retention.
