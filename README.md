# dumbllmproxy

A small local LLM request logger. It transparently proxies arbitrary LLM HTTP endpoints, captures complete logical
requests/responses in SQLite, groups them into sessions, reconstructs OpenAI Chat Completions streams, and provides a
local web UI for inspecting conversations and HTTP exchanges.

## Features

- Provider-agnostic forwarding: method, path, query, headers and body.
- Configurable path prefixes mapping the proxy to arbitrary upstream base URLs.
- Transparent streaming passthrough with final OpenAI Chat Completions reconstruction; no SSE event log.
- Raw decoded fallback for unknown or incomplete streams.
- Session and harness detection via configurable request headers.
- Separate Sessions and Requests views, with a conversation tree, HTTP timeline, and request inspector.
- Search, filters, sorting, pagination, and URL-preserved list state.
- Token usage and cost, including provider-reported and calculated costs.
- Single Go executable with embedded React frontend and local SQLite database.
- Binds to localhost; no authentication.

## Build

Requirements: Go 1.26+, Node.js/npm.

```sh
make
```

`make` installs the frontend dependencies, builds the bundle, and compiles the binary. It only rebuilds what changed.

Individual targets:

```sh
make frontend   # npm install + vite build
make build      # frontend + go build
make test       # go test ./...
make clean      # remove node_modules and the binary
make distclean  # also remove web/dist
```

The frontend is embedded via `go:embed`. `web/dist` is generated and ignored by git; only a placeholder is committed so
a fresh checkout still compiles. If you build without the frontend, the UI responds with a short "not built" message.

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

The proxy forwards request headers unchanged. Put provider credentials in the harness/client as usual.

## Routing

Upstreams are matched by the longest configured path prefix. The prefix is stripped and the remaining path/query is
appended to `base_url`:

```text
/openrouter/v1/chat/completions?x=1
  -> https://openrouter.ai/api/v1/chat/completions?x=1
```

`/api` is reserved for the local UI API. Unmatched paths are served by the UI (or return a 404 for API paths).

## What is captured

Each conversation request stores its request and final response bodies, status, content types, model, session, harness,
configured metadata headers, token usage, and cost. Full headers are not stored. Auxiliary traffic (model listings,
presets, health checks) is not stored.

Streaming responses are passed through in real time; the final OpenAI Chat Completions response is reconstructed and
stored. No individual SSE events are stored.

## Configuration

See `config.example.yaml` for the full reference. The main sections are:

- `proxy.listen`, `proxy.database`
- `upstreams[]`: `name`, `prefix`, `base_url`, `parser`, `pricing`
- `session_headers`, `harness_headers`, `metadata_headers`
- `pricing.deepseek`: unit, time zones, holidays, and per-model rates

`session_headers` and `harness_headers` are ordered detection lists; the first present non-empty header wins. Only the
extracted values are stored. `metadata_headers` is an independent capture list; entries are exact names or `re:`
regular expressions.

## Documentation

- `docs/design.md` — invariants and design decisions.
- `config.example.yaml` — configuration reference.

## Tests

```sh
make test
```

## License

GPL-3.0-only. See [LICENSE](LICENSE).
