# TODO

## Storage

The database still grows roughly quadratically because every request stores the full message history:

- Store only the messages a request adds, plus a reference to the request it extends, and reconstruct the full body on
  read. This turns quadratic history storage into linear unique-message storage. Bigger change, and it conflicts with
  storing the raw body verbatim, so decide whether a raw copy is still needed.

## SQL offload

Prefer SQL for filtering, sorting, pagination, aggregation, counting and distinct values across the store. Keep Go for
work SQL cannot express cleanly: provider-specific parsing, stream reconstruction and cost calculation. This applies
beyond list views; the items below are instances of the same direction.

- Move request filtering, sorting and pagination into SQL. `ListAllRequestsFiltered` currently loads every request, then
  filters and sorts in Go (`internal/store/store.go`).
- Move session filtering, sorting and pagination into SQL. `ListSessions` / `ListSessionsFiltered` aggregate all
  sessions and all requests in memory.
- Limit cost resolution to the rows a read actually needs. `sessionCosts` walks the whole `requests` table on every
  session list or session detail read, and `GetSession` resolves costs for every session, not just the requested one.
  The calculation itself stays in Go.
- Index the columns used by `Facets` and avoid unindexed `SELECT DISTINCT` on every call.

## Performance

- Re-parsing every request body in `openai.ExtractMessages` is the other half of the tree build. It is inherent to
  storing the full history per request; the storage deduplication item is the fix, not the tree builder.
- Replace the linear child scan in `conversation.findOrCreate` (`internal/conversation/tree.go`) with a hash-keyed map.
  Secondary: it only matters when a node has many children.

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

## Data and configuration

- Fill in the real DeepSeek rates and holidays in `config.yaml`. The example rates are placeholders (`0.00`).

## Housekeeping

- Add `.gitattributes` (`* text=auto eol=lf`, mark `web/dist/**` and `web/package-lock.json` as generated).
- Add CI (build, `go vet`, `go test ./...`, frontend build).
- Add keyboard shortcuts for list navigation and the request drawer.
