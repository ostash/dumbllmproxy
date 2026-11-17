# Design invariants

This document records the non-obvious invariants behind dumbllmproxy. It is not a feature list or a user guide; see
`README.md` and `config.example.yaml` for those. Behavior is ultimately defined by the code and tests.

## Scope

- A local, single-user request logger, not a general-purpose LLM gateway.
- No authentication, multi-user support, plugin system, or credential management.
- Accept only `localhost` and the machine's local interface addresses in the Host header. This blocks DNS rebinding; it
  is not authentication.
- Provider-agnostic forwarding. Provider-specific parsing, usage extraction, and cost calculation must not affect
  forwarding.

## Forwarding

- Route by longest configured path prefix. Strip the prefix and append the remaining path and query to `base_url`.
  `/api` is reserved; `/` is rejected.
- Forward method, path, query, headers, body, status, and response headers. Handle hop-by-hop headers in both
  directions; drop `Connection` and every header it names.
- Do not inject or manage credentials. Forward client headers unchanged.
- Buffer and re-frame the request body as needed; reject connection upgrades.
- Forward compressed bytes unchanged; store a separately decoded copy.
- Do not follow redirects. Forward 3xx responses and their `Location` header unchanged.
- Stream all responses as they arrive, regardless of content type. Do not buffer the whole response before forwarding.
  The capture buffer is bounded; exceeding it truncates the capture, never the forwarded response.
- Never present a truncated upstream response as complete. Do not turn a completed stream or a fully read body into a
  failure.

## Capture

- Classify from the request body before forwarding. Only conversation requests are captured; auxiliary traffic is
  forwarded without buffering and discarded. There is no response-based fallback: an unrecognized request body is
  auxiliary.
- Store only the extracted session and harness values, not header names or raw header values.
- Capture only configured metadata headers; never store the full request/response header set.
- Store request/response bodies, status, content types, model, session, harness, configured metadata, usage, and
  provider-reported cost.
- Capture runs asynchronously after the response is forwarded and must not block the response path under normal
  operation. The bounded capture queue applies backpressure rather than dropping captures when full.

## Streaming reconstruction

- Reconstruct OpenAI Chat Completions streams into one final JSON response. Do not store individual SSE events.
- A recognized stream is complete only with `[DONE]` or a terminal error object. A `finish_reason` alone is not enough.
- Incomplete or malformed recognized streams store the partial reconstruction plus the decoded raw stream as a fallback,
  and are marked incomplete.
- Unknown stream formats store the decoded concatenated stream as one raw fallback.
- A read error after a complete stream (for example, the client closing after `[DONE]`) does not mark the capture
  incomplete.

## Conversation tree

- Build a tree from completed exchanges only. Shared message-history prefixes merge; diverging histories branch.
- A node records the first request that introduced it and all requests where it appears.
- Failed or unparseable requests stay in the session request list and timeline but do not add dangling turns.
- Message identity ignores provider-specific reasoning and annotation fields.

## Cost

- Provider-reported cost is a capture fact and takes precedence.
- Calculated cost is derived at read time from capture facts and the active configuration. It is never stored.
- Changing pricing configuration and restarting corrects historical totals without migration or replay.
- Costs are USD only.
- DeepSeek schedule rules are provider-specific. Do not build a universal pricing DSL.

## Storage

- SQLite with WAL and foreign keys; capture writes must not block UI reads.
- Payloads are compressed on disk and decoded on read; older rows remain readable.
- Do not persist calculated costs, cost explanations, or pricing snapshots.
- One-time migration scripts are run manually and are not committed.

## UI

- Two top-level views: Sessions and Requests.
- A session combines a conversation tree and an HTTP request timeline, synchronized by request ID.
- Unassigned requests are not a session and are reachable only through the Requests view.
- Refreshing is manual. Do not auto-refresh an open inspector.
- List state is preserved in the URL.

## Non-goals

- General-purpose gateway, routing, load balancing, or policy features.
- Multi-user or hosted service.
- Authentication or access control.
- Plugin or extension system.
- Harness-specific integration.
- SSE event recorder or viewer.
- Elaborate provider abstraction or provider-specific forwarding.
- Credential management or injection.
- Full HTTP header capture.
- Additional stream formats or cost calculators without a concrete need.
