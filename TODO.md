# TODO

## Storage

The database still grows roughly quadratically because every request stores the full message history:

- Store only the messages a request adds, plus a reference to the request it extends, and reconstruct the full body on
  read. This turns quadratic history storage into linear unique-message storage. Bigger change, and it conflicts with
  storing the raw body verbatim, so decide whether a raw copy is still needed.
- Do not store bodies for auxiliary requests (model listings, presets), or truncate them. They are rarely useful and add
  noise.

## Performance

- Move request filtering, sorting and pagination into SQL. `ListAllRequestsFiltered` currently loads every request, then
  filters and sorts in Go (`internal/store/store.go`).
- Move session filtering, sorting and pagination into SQL. `ListSessions` / `ListSessionsFiltered` aggregate all
  sessions and all requests in memory.
- Avoid resolving the cost of every request on every list call. `sessionCosts` walks the whole `requests` table each
  time a session list or session detail is read.
- Replace the linear child scan in `conversation.findOrCreate` (`internal/conversation/tree.go`) with a hash-keyed map.
  It is O(n²) in the number of messages per parent.
- Cache or index `Facets` instead of running `SELECT DISTINCT` on every call.

## API and UI

- Serve request and response bodies from a dedicated endpoint and load them lazily in the inspector instead of embedding
  them in the detail response.
- When the browser advertises `zstd` in `Accept-Encoding`, pass the stored compressed blob through with
  `Content-Encoding: zstd` instead of decompressing and re-encoding it. Fall back to server-side decompression.

## Conversation and session UI

- Search inside the conversation tree.
- Expand/collapse all branches.
- Collapse large branches by default, with a per-branch toggle.
- Lazy-load conversation subtrees.
- Stable conversation node IDs across rebuilds. IDs are currently `n1`, `n2`, … assigned per build.
- Virtualize long conversations and timelines.
- Paginate the session request timeline. It currently renders every request in the session.

## Lists and filters

- Add a harness filter to the Sessions view (currently Requests only).
- Export a session or request as JSON.

## Design questions

- **Unassigned as a forest of conversations.** The tree builder already separates unrelated requests into distinct
  roots. A dedicated view could show one card per root (request count, tokens, cost, time range) instead of a single
  aggregate. Approaches not decided yet.
- **Auxiliary capture control.** Optional `ignore_paths` (do not capture) or `auxiliary_paths` (capture but never
  session) config, so model listings and presets can be kept out of the database entirely.
- **Facets and auxiliary traffic.** `/api/facets` currently includes providers and models seen only in auxiliary
  requests. Decide whether filter suggestions should be limited to conversation requests.

## Data and configuration

- Fill in the real DeepSeek rates and holidays in `config.yaml`. The example rates are placeholders (`0.00`).
- Document the one-time database migration for existing databases: add the `conversation` column, backfill it from the
  request and response bodies, and unlink auxiliary requests from sessions. The project intentionally has no migration
  code.

## Housekeeping

- Add `.gitattributes` (`* text=auto eol=lf`, mark `web/dist/**` and `web/package-lock.json` as generated).
- Add CI (build, `go vet`, `go test ./...`, frontend build).
- Add keyboard shortcuts for list navigation and the request drawer.
