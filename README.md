# dumbllmproxy

A small local LLM request logger. It transparently proxies arbitrary LLM HTTP endpoints, captures complete logical
requests/responses in SQLite, groups them into sessions, reconstructs OpenAI Chat Completions streams, and provides a
local web UI for inspecting conversations and HTTP exchanges.

## Features

- Provider-agnostic forwarding: method, path, query, headers and body.
- Configurable path prefixes mapping the proxy to arbitrary upstream base URLs.
- Transparent streaming passthrough, with final OpenAI Chat Completions stream reconstruction. No SSE event log is
  stored.
- Raw decoded fallback for unknown stream formats or failed reconstruction.
- Session grouping via configurable request headers.
- Separate Sessions and Requests views; requests can be browsed as individual generations without session grouping.
  Opening a request shows its reconstructed conversation with turns, tool calls and tool results.
- Paginated Sessions and Requests lists (50/100/200 rows) with rolling time filters from the last hour through the last
  year.
- Server-side search across the whole result set, plus quick filters for failed responses, incomplete captures,
  streaming, tool calls, unassigned requests, slow requests, and a specific provider, model, or harness. Provider/model/
  harness inputs suggest known values. Active filters can be cleared in one click.
- Pagination with first/last and a direct page jump.
- Sortable request duration and a "slow requests" filter.
- Session-level failed/incomplete counts and filters.
- Relative timestamps in lists and the timeline, with absolute times on hover.
- Previous/next request navigation inside the request inspector.
- Copy controls for request IDs and upstream URLs; conversation nodes are linkable via `#/session/{id}?node=...`.
- Manual refresh preserves the selected conversation node, open drawer, and inspector pane. A "new captures" badge
  appears when polling detects new requests; nothing refreshes automatically.
- JSON payloads can be expanded or collapsed in bulk.
- The request drawer is a proper dialog with focus trapping and restores focus on close.
- List state (page, window, search, filters, sort) is kept in the URL, so returning from an inspector restores the exact
  list view.
- Request duration in the list, timeline, drawer and inspector.
- Long conversation text, reasoning, tool arguments and tool results can be expanded in place; nothing is silently
  truncated.
- Session totals show no value when no request reported usage or cost, and are marked partial when only some requests
  did.
- Conversation tree + HTTP request timeline, synchronized by request ID.
- Requests link to their session without losing the row click.
- Raw request/response body inspection, parsed JSON, usage and cost breakdown, including input, cached, non-cached,
  generated and reasoning tokens when reported.
- OpenRouter provider-reported cost (`usage.cost`).
- DeepSeek calculated cost from configurable, per-model rate rules with first-match conditions on from/to dates,
  day/time, explicit dates and holidays.
- Single Go executable with embedded React frontend and local SQLite database.
- Binds to localhost; no authentication.

## Build

Requirements: Go 1.26+, Node.js/npm.

```sh
make
```

`make` installs the frontend dependencies, builds the bundle, and compiles the binary. It only rebuilds what changed, so
re-running it is cheap.

The frontend is embedded via `go:embed`. `web/dist` is generated and ignored by git; only a placeholder is committed so
a fresh checkout still compiles. If you build without the frontend, the UI responds with a short "not built" message
instead of a blank page.

Individual targets:

```sh
make frontend   # npm install + vite build
make build      # frontend + go build
make test       # go test ./...
make clean      # remove node_modules and the binary
make distclean  # also remove web/dist
```

To build the frontend by hand:

```sh
cd web && npm install && npm run build && cd ..
go build -o dumbllmproxy .
```

## Run

```sh
cp config.example.yaml config.yaml
./dumbllmproxy -config config.yaml
```

Then open <http://127.0.0.1:8080/>.

Point your harness at the proxy path instead of the provider URL:

```text
http://127.0.0.1:8080/openrouter/v1/chat/completions
http://127.0.0.1:8080/deepseek/chat/completions
```

The proxy forwards the request headers unchanged. Put provider credentials in the harness/client as usual.

## Routing

Upstreams are matched by the longest configured path prefix. The prefix is stripped and the remaining path/query is
appended to `base_url`:

```text
/openrouter/v1/chat/completions?x=1
  -> https://openrouter.ai/api/v1/chat/completions?x=1
```

`/api` is reserved for the local UI API. Unmatched paths are served by the UI (or return a 404 for API paths).

## Capture behaviour

Stored per HTTP exchange:

- Request start/completion timestamps, HTTP method, local path, upstream URL.
- Provider (from the matched upstream config) and model (from request JSON).
- HTTP status and content types.
- Complete request body, decoded if compressed.
- Complete final response body, decoded if compressed.
- Reconstructed final JSON for recognized OpenAI Chat Completions streams.
- Raw decoded stream fallback when a stream is unknown, malformed, or incomplete.
- Token usage and cost data when available.
- Session identifier and the source header name.
- Harness name derived from the configured `x-harness-id` metadata header, then `x-openrouter-title`, then the
  User-Agent product token (for example `pi/1.5.0` → `pi`).
- Configured metadata headers only. Full headers are not stored. There is no built-in sensitive-header list: whatever
  you configure is what gets stored.

Compressed request/response bytes are forwarded unchanged to preserve normal client/upstream behaviour; a decoded copy
is stored for inspection and parsing.

Hop-by-hop headers are not forwarded in either direction: `Connection` and every header it names (case-insensitively,
across multiple `Connection` lines), plus the standard hop-by-hop set. Ordinary end-to-end headers and repeated values
are preserved.

### Streaming

For streaming responses the proxy writes upstream chunks to the client as they arrive, flushes each chunk, and
accumulates the raw stream internally. When the stream finishes, it reconstructs a final `chat.completion` JSON object
and stores that once. Individual SSE events are never persisted.

Streaming detection is based on the response `Content-Type` containing `text/event-stream`; other content types are
treated as non-streaming and buffered before forwarding.

A recognized stream is complete only when it ends with `[DONE]` (or a terminal error object). A `finish_reason` without
`[DONE]` is treated as incomplete, because the connection may have been cut before the final frames. Incomplete captures
are marked `incomplete`; the partial reconstruction is stored as the response body and the decoded raw stream as a
separate fallback payload.

## Sessions

Configure one or more request header names in priority order:

```yaml
session_headers:
  - x-session-id
  - x-conversation-id
```

The first present non-empty header wins. The identifier and header name are stored. Requests without a session header
appear under **Unassigned**.

## Provider fields used

OpenRouter:

- `usage.cost` in the response JSON, or in the final reconstructed stream chunk.
- `usage.prompt_tokens_details.cached_tokens` and `usage.prompt_tokens_details.cache_write_tokens`.
- `usage.completion_tokens_details.reasoning_tokens` or `usage.output_tokens_details.reasoning_tokens`, when present.
- Original usage data, including `cost_details`, is preserved as raw JSON.

DeepSeek:

- `usage.prompt_cache_hit_tokens`
- `usage.prompt_cache_miss_tokens`
- `usage.completion_tokens`
- `usage.completion_tokens_details.reasoning_tokens`, when present

DeepSeek's OpenAI-compatible chat endpoint is `/chat/completions` on `https://api.deepseek.com`; thus the proxy example
above has no `/v1` segment.

DeepSeek cost is calculated as:

```text
(prompt_cache_hit_tokens  * input_cache_hit_rate
 + prompt_cache_miss_tokens * input_cache_miss_rate
 + completion_tokens * output_rate) / 1_000_000
```

If cache token fields are missing, cost is left unavailable rather than guessed. Rates are configured per model under
`pricing.deepseek.models`. Rates are evaluated in configuration order, and the first matching rate wins. Conditions can
include inclusive `from`/`to` dates (`to` optional), days of week, half-open local time windows, holiday
inclusion/exclusion, and explicit `dates` / `exclude_dates`. A rate without conditions matches everything. Holiday dates
are configured under `pricing.deepseek.holidays` and evaluated in `holiday_timezone`. The model is taken from the
request body, and an unconfigured model leaves cost unavailable.

## Calculated cost is derived at read time

Calculated costs are never stored as authoritative values. The database keeps only capture facts: request timestamp,
model, normalized and raw usage, response payload, and the configured pricing name. Request details, request lists,
session details and session summaries recompute the cost, cost explanation and pricing snapshot on every read, using the
active configuration and the original request timestamp to pick the historical rate.

Editing the pricing YAML and restarting therefore corrects all previously captured request and session totals without
migration or replay. Provider-reported costs such as OpenRouter's `usage.cost` are capture facts and stay persisted.

Session totals are formed from freshly calculated request costs and cost sorting in both list views uses those fresh
values. Costs are assumed to be USD; no currency conversion is attempted.

Calculators live behind a small registry keyed by the configured `pricing` name (`internal/pricing`); the generic proxy
does not reference DeepSeek directly. At startup the DeepSeek schedule is checked for likely mistakes (no catch-all
rate, an earlier broad rule shadowing later ones, or date coverage gaps) and any findings are logged as warnings.

## Configuration reference

See `config.example.yaml`. Key sections:

- `proxy.listen`, `proxy.database`
- `upstreams[]`: `name`, `prefix`, `base_url`, `parser`, `pricing` (prefix `/` is rejected because it would shadow the
  UI)
- `parser`: `openai_chat` or `none`
- `pricing`: `openrouter`, `deepseek`, or `none`
- `session_headers`, `metadata_headers`
- `pricing.deepseek`: unit, schedule/holiday timezones, holidays, and per-model rates with optional conditions

## Tests

```sh
make test
```

The Go tests cover prefix routing, OpenAI stream reconstruction, incomplete and malformed streams, DeepSeek pricing,
conversation-tree branching/occurrence mapping, proxy forwarding, compressed passthrough, session extraction, and the
HTTP API.

## License

GPL-3.0-only. See [LICENSE](LICENSE).
