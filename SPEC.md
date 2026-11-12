# LLM Request Logger — Specification

## Goal

Build a small, local tool for capturing and inspecting LLM interactions. It is intended for personal use, not as a
general-purpose LLM gateway.

The tool should make it easy to understand:

- What an agent sent to an LLM, including system prompts and tool definitions.
- What the LLM returned, including tool calls and reasoning content when provided by the API.
- How requests relate to an agent session.
- Token usage and cost, when available.

**Core principle:** forwarding is provider-agnostic. Provider-specific parsing, usage extraction, and cost calculation
must not be required for forwarding and must not change the traffic sent to or received from the harness.

## Proxy behaviour

### Routing and forwarding

- Configure upstreams with a path prefix, base URL, parser selection, and pricing selection. The root prefix `/` is
  rejected because it would shadow the UI.
- Select an upstream using longest-prefix matching. Match path prefixes at path boundaries; for example, `/llm` matches
  `/llm` and `/llm/...`, but not `/llm-other`.
- Strip the matched prefix and append the remaining path and query string to the upstream base URL.
- Forward the HTTP method, path, query, headers, and body. Forward the upstream status, headers, and body.
- Set the upstream `Host` header to the target host and handle hop-by-hop headers according to standard HTTP proxy
  rules. In both directions, drop `Connection` and every header it names, in addition to the standard hop-by-hop
  headers. Preserve ordinary end-to-end headers and repeated values.
- Preserve normal request/response semantics and support streaming.
- Forward client-supplied credentials unchanged. Do not inject, replace, or manage provider credentials.
- Forward compressed request and response bytes unchanged. Store a separately decoded copy for inspection and parsing.
- Unknown providers and payload formats must still work as transparent proxies.

Provider-specific logic must stay outside the forwarding path. Parsing, stream reconstruction, usage extraction, and
cost calculation happen alongside or after forwarding and must not alter the bytes, status, or headers sent to the
harness.

### Request and response capture

For each request, store:

- Request start and completion timestamps.
- Upstream/provider identification from the matched configuration.
- Model, when identifiable from the request body.
- HTTP method, local request path, and upstream URL.
- Complete request body and final logical response body, decoded when possible.
- HTTP status and request/response content types.
- Session and metadata header values selected by configuration.
- Token usage and cost information, when available.
- Session association, when identifiable.

Do not store the full request or response header sets. Extract only configured session and metadata headers, plus
content types and HTTP status as dedicated fields. What is stored is exactly what the user configures; there is no
built-in sensitive-header list.

Preserve captured payloads so they can be inspected later. Extract commonly used fields for presentation and aggregation
where possible. If a captured body cannot be decoded, retain the original bytes for inspection.

## Streaming behaviour

**Streaming must be transparent passthrough.**

When the harness requests a streaming response:

- Forward the upstream stream to the harness as it arrives.
- Do not buffer the complete response before forwarding it.
- Collect a separate copy internally and reconstruct the final logical response.
- Do not persist individual SSE events or provide an SSE event viewer.
- Store the reconstructed response once the stream completes.

Initially support reconstruction of OpenAI Chat Completions SSE. Preserve available information, including:

- Text content.
- Tool calls, including names and arguments.
- Reasoning content, if provided.
- Usage information and other relevant response metadata.

For recognized streams, successful completion requires the `[DONE]` marker and valid data frames. A terminal error
object also ends the stream. A `finish_reason` without `[DONE]` is treated as incomplete, because the connection may
have been cut before the final frames. Store the reconstructed final JSON as the response body; do not also store the
raw SSE stream. If the stream is incomplete or malformed, store the partial reconstruction as the response body and the
decoded raw stream as a separate fallback payload. Mark the capture as incomplete; never present the partial
reconstruction as complete.

For unrecognized stream formats, store the decoded concatenated stream as one raw fallback payload. Do not attempt to
interpret it or store individual SSE events.

Streaming detection is based on the response `Content-Type` containing `text/event-stream`. Responses with other
content types are treated as non-streaming and buffered before forwarding.

Non-streaming requests should be forwarded normally and their complete decoded responses stored. The proxy must not
change the behaviour expected by the harness merely to simplify logging.

## Session identification

Group requests into sessions so an agent conversation can be inspected together.

Harnesses may provide session identifiers in HTTP headers. Support configurable, ordered header mappings to extract
them:

- Do not require a custom harness plugin or assume one universal session header.
- The first configured header with a present, non-empty value wins.
- Preserve the extracted identifier and the header name it came from.
- Do not use routing or affinity headers as session identifiers by default.
- If no identifier is available, keep the request accessible as unassigned. Unassigned requests are not a session:
  unrelated requests must not be aggregated into a pseudo-session or reconstructed as one conversation.

Only conversation requests join sessions. Classify each captured exchange as either a **conversation** request or
**auxiliary** traffic:

- A conversation request carries model input: a non-empty `messages`, `input`, `prompt`, or `contents` field in its
  request body, or a generation response with a non-empty `choices` or `output` field.
- Everything else is auxiliary: model listings, presets, health checks, and similar metadata calls.

Auxiliary traffic is still captured and inspectable, but it never joins a session, even when the client sent a session
header. Its session identifier is not recorded, so it cannot appear to belong to a session it is not part of. This keeps
the unassigned set limited to real generations that lack a session identifier.

## Usage and cost

Cost handling is provider-specific; request forwarding is not. Use provider-specific parsers or rules to extract usage
and cost information where needed. Unknown providers must still work as proxies, with their requests and responses
captured.

Capture the following when available:

- Input tokens.
- Output tokens.
- Cached input tokens.
- Non-cached input tokens, derived from input minus cached input when both are known.
- Reasoning tokens, when reported. Reasoning tokens are a subset of output tokens.
- Cache-read and cache-write tokens, if separately reported.
- Total tokens.
- Provider-reported cost.

If usage or cost cannot be determined, leave it unavailable rather than guessing. Do not treat missing values as zero.

### OpenRouter

- Use the cost reported in `usage.cost` when available.
- Extract cached input and cache-write tokens from `usage.prompt_tokens_details.cached_tokens` and
  `usage.prompt_tokens_details.cache_write_tokens`, when present.
- Preserve the original usage data, including `cost_details`.
- Leave cost unavailable when `usage.cost` is absent.
- For streaming responses, extract usage from the final reconstructed response when present.

### DeepSeek

Calculate cost using configurable pricing.

Rates are configured per model. Take the model from the request body. An unconfigured model leaves cost unavailable
rather than falling back to another model's rates.

Rates are expressed per one million tokens. Costs are reported in USD. DeepSeek has three rate categories:

- Input tokens (cache hit).
- Input tokens (cache miss).
- Output tokens.

Use this formula:

```text
(prompt_cache_hit_tokens  * input_cache_hit_rate
 + prompt_cache_miss_tokens * input_cache_miss_rate
 + completion_tokens       * output_rate) / 1_000_000
```

Pricing configuration requirements:

- A model-to-rates mapping.
- Each rate contains cache-hit, cache-miss, and output rates.
- Each rate has optional conditions describing when it applies. When multiple condition types are specified, all must
  match; any configured time window may satisfy the time condition.
- Supported conditions: inclusive `from`/`to` dates, day of week, one or more local-time windows, holiday
  inclusion/exclusion, and explicit `dates`/`exclude_dates`.
- `to` is optional and means there is no upper date bound.
- Time windows are half-open: `start <= t < end`.
- A rate without conditions matches everything.
- Evaluate date, weekday, and time conditions against the request start time. Rates are evaluated in configuration
  order; the first matching rate wins. List more specific rates before defaults.
- Holidays are configured provider-wide and evaluated in the configured holiday time zone.
- Provider-wide settings supply rate unit, schedule time zone, holiday time zone, and holiday dates.
- DeepSeek peak pricing is expressed as conditional rates in configuration, not hardcoded peak/off-peak logic.
- Required DeepSeek fields are `usage.prompt_cache_hit_tokens`, `usage.prompt_cache_miss_tokens`, and
  `usage.completion_tokens`.
- If any required token field is missing, or the request model has no configured rates, leave cost unavailable rather
  than guessing.

Keep pricing configuration separate from application code.

### Calculated cost is derived, not captured

Calculated costs are never authoritative capture-time values. Persist only the immutable capture facts: request
timestamp, model, normalized usage/token counts, raw usage, and the response payload. Recalculate calculated cost,
cost explanation, and pricing snapshot whenever request details, request lists, session details, or session summaries
are read, using the active configuration and the original request timestamp to select the historical rate.

A changed pricing configuration must therefore correct previously captured request and session totals after restart,
without any data migration or replay. Requests with insufficient usage or no matching model/rate continue to show
unavailable cost rather than a guess.

Provider-reported costs, such as OpenRouter's `usage.cost`, are captured facts and may stay persisted.

### Calculator boundary

Select calculators through a registry keyed by the configured pricing name. Each calculator receives the capture facts
needed for evaluation — at minimum model, normalized usage, and capture timestamp — and returns a generic cost result
with explanation and snapshot. The generic proxy must not own a provider calculator or grow a provider switch.

Do not create a universal pricing DSL. Keep DeepSeek's schedule logic provider-specific; add another calculator only
when another provider actually needs calculated pricing.

### Schedule diagnostics

Report likely configuration mistakes at startup where practical: a model with no catch-all rate, an earlier broad rule
that shadows later rules, and date coverage gaps before the earliest or after the latest configured bound.

## User interface

The main purpose of the UI is session inspection. Provide two top-level views: **Sessions** and **Requests**.

### Sessions view

Show a list of sessions with:

- Session identifier.
- Start time and last activity.
- Request count.
- Provider/model information, when available, in a stable sorted order.
- Failed and incomplete request counts, when non-zero.
- Total token usage, with a breakdown of input, cached input, non-cached input, generated output, and reasoning tokens
  when available.
- Total cost, formed from freshly calculated request costs. Make clear when usage or cost data is missing from some
  requests: show no value when no request reported it, and mark aggregates as partial when only some requests did.
- Costs are assumed to be USD; no currency conversion is attempted.

Allow sorting and filtering. Paginate with 50, 100, or 200 rows per page. Provide rolling time filters: last 1h, 3h,
6h, 12h, 1d, 2d, 1w, 1mo, and 1y.

Search must cover the whole result set, not just the current page. Match session identifier, provider, and model.
Provide quick filters for sessions with failures, sessions with incomplete captures, and a specific provider or model. Provider and model inputs should suggest known values. Show the number of active filters and offer a
one-click reset, including from the empty state.

Conversation requests without a session identifier must remain inspectable through the Requests view's **Unassigned**
filter, reachable directly from the top navigation. They are not listed as a session, because unrelated requests do not
form a conversation and their combined totals would be meaningless. Auxiliary traffic is excluded from that filter.

### Requests view

Provide a second top-level view listing individual HTTP exchanges/model generations without session grouping.

Show:

- Started time.
- Request ID and HTTP status.
- Session identifier, or an unassigned marker.
- Provider and model.
- Harness name, when it can be derived from configured metadata headers. Prefer `x-harness-id`, then OpenRouter's
  `X-OpenRouter-Title`, then the User-Agent product token.
- Method and local path.
- Request duration, derived from start and completion timestamps, sortable and filterable (for example, slow requests).
- Token usage and cost, when available.
- An auxiliary marker for non-generation traffic.

Allow filtering and sorting, including sorting by freshly calculated cost. Use the same page sizes and rolling time
windows as the Sessions view. Selecting a request opens the request inspector.

Search must cover the whole result set, not just the current page. Match request ID, session identifier, provider,
model, harness, method, path, and status. Provide quick filters for failed responses, incomplete captures, streaming
responses, requests with tool calls, unassigned conversation requests, auxiliary traffic, slow requests, and a specific
provider, model, or harness. Provider, model, and harness inputs should suggest known values. The unassigned and
auxiliary filters are disjoint: unassigned means a conversation request without a session, auxiliary means
non-generation traffic.

Assigned sessions must be reachable from the request row without losing the row's own click target.

Pagination must offer first/last and a direct page jump in addition to previous/next.

### Session inspector

Combine two synchronized representations:

**Conversation view**

- Reconstruct the readable conversation from captured requests and responses.
- Display messages, tool calls, and tool results in their logical order.
- Represent the conversation as a tree:
  - Shared message-history prefixes merge into common nodes.
  - Diverging histories create branches.
  - Assistant response messages and tool calls attach to the request that produced them.
  - A node records the first request that introduced it as its source request.
  - A node records all requests in which the message appears.
- Conversation reconstruction is available for recognized `openai_chat` payloads. Unknown formats remain inspectable
  through raw request/response payloads.
- Only completed exchanges contribute to the conversation tree. A request that failed (for example a 401 or 404) or
  whose response could not be parsed has no assistant message, so it must not add a dangling turn. Such requests remain
  visible in the session's request list, timeline, and failed count.
- Long message text, reasoning, tool arguments, and tool results may start compact, but every truncated block must offer
  a control to show the full content. Do not silently drop conversation material.

**HTTP request timeline**

- Show every captured HTTP request in chronological order.
- Identify upstream, model, harness, timestamp, status, and duration where available.
- Selecting a request highlights the messages it introduced.
- Selecting a conversation element highlights all requests where it appears; its source request is reachable from the
  element. The selected element is reflected in the URL so it can be linked to.

Make each conversation element traceable to its underlying request. Make clear that the displayed conversation is
reconstructed from multiple HTTP exchanges.

### Request inspector

For an individual request, provide:

- A **Conversation / Payload** switch. Default to Conversation when a parsed conversation exists; otherwise default to
  Payload.
- Previous/next navigation through the current request list, when the inspector was opened from it.
- Copy controls for the request ID and upstream URL.
- A reconstructed conversation from that request's message history and final response, including recognized tool calls
  and tool results.
- A payload view: pretty-printed, collapsible JSON for chat completions and raw text for other formats.
- Copy-to-clipboard controls for payloads.
- Relevant extracted HTTP metadata, status, and duration.
- Token usage breakdown for input, cached input, non-cached input, generated output, and reasoning tokens, when
  available.
- Inline usage and cost details, including raw usage, cost explanation, and pricing snapshot JSON when available.
- The fallback decoded raw stream when a stream was incomplete or unrecognized.

Unknown API formats must remain inspectable through their raw payloads. No SSE event viewer is required.

### List state and refresh

Preserve list context when opening an inspector: returning to a list must restore its page, time window, search, quick
filters, and sort. Encode list state in the URL or retain it in application state.

Refreshing is manual. Do not auto-refresh an open inspector, because that would clear selection state and close the
request drawer. A manual refresh must preserve the selected conversation node, the open request drawer, and the
inspector pane. Show a "new captures available" indicator when polling detects new requests; do not refresh
automatically.

## Persistence

Use a local SQLite database.

Store:

- Sessions.
- Requests and responses.
- Extracted metadata and usage.
- Configured session/metadata header values.
- The configured pricing name per request, as a capture fact.
- Provider-reported cost, when the provider reports one.

Do not persist calculated costs, cost explanations, or pricing snapshots; they are derived at read time.

Store decoded payloads. They may be minified and compressed, but must be restored to the same logical payload on read.
Compression is detected from the stored bytes, so rows written before compression remain readable.

Avoid unnecessary duplication:

- Store each request/response payload once.
- Store timestamps in a fixed-width UTC format so they sort chronologically as text.
- Store a raw stream fallback only when reconstruction is incomplete, malformed, or unsupported.
- Do not store individual SSE events.

Do not store full request/response headers. Only configured session and metadata headers are stored.

The application must work locally without an external database or cloud service.

## Configuration

Use a simple, human-editable YAML configuration file supplied through a command-line flag. Read configuration at
startup; restart to reload it.

Configure:

- Listening address and port.
- SQLite database path.
- Upstream prefixes, base URLs, parser selections, and pricing selections.
- Session header mappings in priority order.
- A metadata header extraction list applied to both requests and responses.
- Provider-specific usage extraction and pricing.
- Time zone and holidays used for pricing.
- Rate unit.

Do not configure or store provider credentials. Credentials are supplied by the client and forwarded as-is.

No authentication is provided. Bind to localhost by default. Secrets should not be exposed in the UI or written to logs.
Full request/response headers are not persisted; only configured session and metadata headers are stored.

## Technology

**Backend**

- Go.
- Standard `net/http`.
- SQLite.
- YAML configuration.

**Frontend**

- React.
- TypeScript.
- Vite.
- Plain CSS. No component library is required for the initial implementation.

Build the frontend into the Go application so the result can run as one local executable. Use straightforward SQL rather
than introducing an ORM without a clear need.

Include unit and integration tests for:

- Prefix routing and forwarding.
- Streaming passthrough and reconstruction.
- Incomplete/malformed stream handling.
- Compressed request/response handling.
- Session header extraction and precedence.
- OpenRouter and DeepSeek cost handling.
- Conversation tree branching and request traceability.
- The local HTTP API.

## Non-goals

Do not build:

- A general-purpose LLM gateway with routing, load balancing, or policy features.
- A multi-user or hosted service.
- Authentication or access control.
- A plugin or extension system.
- A harness-specific integration.
- An SSE event recorder or viewer.
- An elaborate provider abstraction.
- Advanced analytics or dashboards beyond what helps inspect sessions.
- Provider-specific forwarding implementations.
- Credential management or credential injection.
- Full HTTP header capture.
- Support for additional stream formats until there is a concrete need.
- Support for additional cost calculations until there is a concrete need.

## Acceptance criteria

The application is useful when:

- It proxies arbitrary configured upstream LLM API endpoints through path prefixes without provider-specific forwarding
  code.
- Requests and responses are forwarded without changing normal harness behaviour, including client-supplied credentials
  and compressed bytes.
- Streaming requests are forwarded in real time.
- Completed OpenAI Chat Completions streams are reconstructed and stored without storing individual SSE events.
- Incomplete recognized streams are marked incomplete and retain a single raw decoded fallback; unrecognized streams are
  retained as one raw fallback.
- Full logical requests and final responses can be inspected.
- Full header sets are not stored; only configured session/metadata headers are extracted.
- Requests are grouped into sessions using ordered, configurable headers. Unassigned requests are reachable through the
  Requests view and are never aggregated into a pseudo-session.
- A session can be inspected through both a readable conversation tree and an HTTP request timeline, and individual
  requests can be browsed without session grouping.
- Conversation elements can be traced to their source request and to all requests where they occur.
- Token usage and cost can be inspected per request and per session when available, including input, cached, non-cached,
  generated, and reasoning token counts where reported.
- OpenRouter-reported costs are used when available.
- DeepSeek costs can be calculated from configurable, per-model ordered rate rules with conditions based on dates, day
  of week, time, explicit dates, and holidays.
- Calculated costs are recomputed on every read, so editing the pricing configuration and restarting corrects
  previously captured request and session totals without migration or replay.
- Session totals and cost sorting use freshly calculated costs; costs are assumed to be USD.
- Missing DeepSeek cache-token data leaves cost unavailable rather than guessed.
- Unknown providers still work as proxies and their raw requests and responses remain inspectable.
- Everything runs locally using one application and a SQLite database.
