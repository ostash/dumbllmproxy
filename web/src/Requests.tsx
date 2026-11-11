// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useEffect, useMemo, useState } from "react";
import { api } from "./api";
import type { Facets, RequestSummary } from "./types";
import {
  formatCost,
  formatDuration,
  formatRelativeTime,
  formatTime,
  formatTokens,
  statusClass
} from "./format";
import { TokenBreakdown } from "./TokenBreakdown";
import { Pagination } from "./Pagination";
import {
  TIME_WINDOW_OPTIONS,
  sinceForWindow,
  type PageSize,
  type TimeWindow
} from "./filters";
import {
  boolParam,
  intParam,
  orderParam,
  pageSizeParam,
  sortParam,
  stringParam,
  timeWindowParam
} from "./listParams";
import { useDebouncedValue } from "./useDebouncedValue";

const SORT_KEYS = ["started_at", "id", "status_code", "total_tokens", "cost_amount", "duration"] as const;
type SortKey = (typeof SORT_KEYS)[number];

const SLOW_SECONDS = 30;

type Props = {
  initialParams: URLSearchParams;
  refreshKey: number;
  onOpenRequest: (id: number) => void;
  onOpenSession: (id: number) => void;
  onListHash: (hash: string) => void;
  onListData: (ids: number[]) => void;
};

export function Requests({ initialParams, refreshKey, onOpenRequest, onOpenSession, onListHash, onListData }: Props) {
  const [requests, setRequests] = useState<RequestSummary[]>([]);
  const [facets, setFacets] = useState<Facets>({ providers: [], models: [], harnesses: [] });
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [reloadKey, setReloadKey] = useState(0);
  const [filter, setFilter] = useState(() => stringParam(initialParams, "q"));
  const [sortKey, setSortKey] = useState<SortKey>(() => sortParam(initialParams, SORT_KEYS, "started_at"));
  const [sortDir, setSortDir] = useState<"asc" | "desc">(() => orderParam(initialParams));
  const [page, setPage] = useState(() => intParam(initialParams, "page", 0));
  const [pageSize, setPageSize] = useState<PageSize>(() => pageSizeParam(initialParams));
  const [timeWindow, setTimeWindow] = useState<TimeWindow>(() => timeWindowParam(initialParams));
  const [failed, setFailed] = useState(() => boolParam(initialParams, "failed"));
  const [incomplete, setIncomplete] = useState(() => boolParam(initialParams, "incomplete"));
  const [streamed, setStreamed] = useState(() => boolParam(initialParams, "streamed"));
  const [toolCalls, setToolCalls] = useState(() => boolParam(initialParams, "tool_calls"));
  const [unassigned, setUnassigned] = useState(() => boolParam(initialParams, "unassigned"));
  const [slow, setSlow] = useState(() => boolParam(initialParams, "slow"));
  const [provider, setProvider] = useState(() => stringParam(initialParams, "provider"));
  const [model, setModel] = useState(() => stringParam(initialParams, "model"));
  const [harness, setHarness] = useState(() => stringParam(initialParams, "harness"));
  const since = useMemo(() => sinceForWindow(timeWindow), [timeWindow]);
  const debouncedFilter = useDebouncedValue(filter);
  const debouncedProvider = useDebouncedValue(provider);
  const debouncedModel = useDebouncedValue(model);
  const debouncedHarness = useDebouncedValue(harness);

  useEffect(() => {
    let cancelled = false;
    api
      .facets()
      .then((f) => {
        if (!cancelled) setFacets(f);
      })
      .catch(() => {
        // Facets are only suggestions; ignore failures.
      });
    return () => {
      cancelled = true;
    };
  }, [refreshKey]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");
    api
      .requests({
        limit: pageSize,
        offset: page * pageSize,
        since,
        sort: sortKey,
        order: sortDir,
        q: debouncedFilter.trim() || undefined,
        status: failed ? "failed" : undefined,
        incomplete: incomplete || undefined,
        streamed: streamed || undefined,
        tool_calls: toolCalls || undefined,
        unassigned: unassigned || undefined,
        min_duration: slow ? SLOW_SECONDS : undefined,
        provider: debouncedProvider.trim() || undefined,
        model: debouncedModel.trim() || undefined,
        harness: debouncedHarness.trim() || undefined
      })
      .then((r) => {
        if (!cancelled) {
          setRequests(r.requests);
          setTotal(r.total);
          onListData(r.requests.map((request) => request.id));
          setLoading(false);
        }
      })
      .catch((e) => {
        if (!cancelled) {
          setError(String(e));
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [
    refreshKey, reloadKey, page, pageSize, since, sortKey, sortDir,
    debouncedFilter, failed, incomplete, streamed, toolCalls, unassigned, slow,
    debouncedProvider, debouncedModel, debouncedHarness
  ]);

  useEffect(() => {
    if (!loading && page > 0 && total <= page * pageSize) {
      setPage(Math.max(0, Math.ceil(total / pageSize) - 1));
    }
  }, [loading, total, page, pageSize]);

  useEffect(() => {
    const params = new URLSearchParams();
    if (page > 0) params.set("page", String(page));
    if (pageSize !== 50) params.set("page_size", String(pageSize));
    if (timeWindow !== "all") params.set("window", timeWindow);
    if (sortKey !== "started_at") params.set("sort", sortKey);
    if (sortDir !== "desc") params.set("order", sortDir);
    if (filter.trim()) params.set("q", filter.trim());
    if (failed) params.set("failed", "1");
    if (incomplete) params.set("incomplete", "1");
    if (streamed) params.set("streamed", "1");
    if (toolCalls) params.set("tool_calls", "1");
    if (unassigned) params.set("unassigned", "1");
    if (slow) params.set("slow", "1");
    if (provider.trim()) params.set("provider", provider.trim());
    if (model.trim()) params.set("model", model.trim());
    if (harness.trim()) params.set("harness", harness.trim());
    const query = params.toString();
    const hash = query ? `#/requests?${query}` : "#/requests";
    window.history.replaceState(null, "", hash);
    onListHash(hash);
  }, [
    page, pageSize, timeWindow, sortKey, sortDir, filter,
    failed, incomplete, streamed, toolCalls, unassigned, slow,
    provider, model, harness, onListHash
  ]);

  function toggleSort(key: SortKey) {
    if (key === sortKey) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir("desc");
    }
    setPage(0);
  }

  function changePageSize(value: PageSize) {
    setPageSize(value);
    setPage(0);
  }

  function changeTimeWindow(value: TimeWindow) {
    setTimeWindow(value);
    setPage(0);
  }

  function toggleFilter(setter: (fn: (v: boolean) => boolean) => void) {
    setter((v) => !v);
    setPage(0);
  }

  const arrow = (key: SortKey) => (sortKey === key ? (sortDir === "asc" ? " ▲" : " ▼") : "");
  const activeFilterCount = [
    filter.trim(), failed, incomplete, streamed, toolCalls, unassigned, slow,
    provider.trim(), model.trim(), harness.trim()
  ].filter(Boolean).length;

  function clearFilters() {
    setFilter("");
    setFailed(false);
    setIncomplete(false);
    setStreamed(false);
    setToolCalls(false);
    setUnassigned(false);
    setSlow(false);
    setProvider("");
    setModel("");
    setHarness("");
    setPage(0);
  }

  return (
    <main className="page">
      <div className="page-heading">
        <div>
          <h1>Requests</h1>
          <p className="muted">Individual HTTP exchanges and model generations, independent of session grouping.</p>
        </div>
        <div className="page-controls">
          <select
            aria-label="Time window"
            value={timeWindow}
            onChange={(e) => changeTimeWindow(e.target.value as TimeWindow)}
          >
            {TIME_WINDOW_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>{option.label}</option>
            ))}
          </select>
          <input
            className="filter"
            aria-label="Search requests"
            placeholder="Search all requests…"
            value={filter}
            onChange={(e) => {
              setFilter(e.target.value);
              setPage(0);
            }}
          />
        </div>
      </div>

      <div className="quick-filters">
        <button className={`filter-chip ${failed ? "active" : ""}`} aria-pressed={failed} onClick={() => toggleFilter(setFailed)}>Failed</button>
        <button className={`filter-chip ${incomplete ? "active" : ""}`} aria-pressed={incomplete} onClick={() => toggleFilter(setIncomplete)}>Incomplete</button>
        <button className={`filter-chip ${streamed ? "active" : ""}`} aria-pressed={streamed} onClick={() => toggleFilter(setStreamed)}>Streaming</button>
        <button className={`filter-chip ${toolCalls ? "active" : ""}`} aria-pressed={toolCalls} onClick={() => toggleFilter(setToolCalls)}>Tool calls</button>
        <button className={`filter-chip ${unassigned ? "active" : ""}`} aria-pressed={unassigned} onClick={() => toggleFilter(setUnassigned)}>Unassigned</button>
        <button className={`filter-chip ${slow ? "active" : ""}`} aria-pressed={slow} onClick={() => toggleFilter(setSlow)}>Slow &gt; {SLOW_SECONDS}s</button>
        <input
          className="filter small-filter"
          aria-label="Provider filter"
          placeholder="Provider"
          list="request-provider-options"
          value={provider}
          onChange={(e) => {
            setProvider(e.target.value);
            setPage(0);
          }}
        />
        <datalist id="request-provider-options">
          {facets.providers.map((value) => <option key={value} value={value} />)}
        </datalist>
        <input
          className="filter small-filter"
          aria-label="Model filter"
          placeholder="Model"
          list="request-model-options"
          value={model}
          onChange={(e) => {
            setModel(e.target.value);
            setPage(0);
          }}
        />
        <datalist id="request-model-options">
          {facets.models.map((value) => <option key={value} value={value} />)}
        </datalist>
        <input
          className="filter small-filter"
          aria-label="Harness filter"
          placeholder="Harness"
          list="request-harness-options"
          value={harness}
          onChange={(e) => {
            setHarness(e.target.value);
            setPage(0);
          }}
        />
        <datalist id="request-harness-options">
          {facets.harnesses.map((value) => <option key={value} value={value} />)}
        </datalist>
        {activeFilterCount > 0 && (
          <button className="filter-chip clear-chip" onClick={clearFilters}>
            Clear {activeFilterCount} filter{activeFilterCount === 1 ? "" : "s"}
          </button>
        )}
        {loading && requests.length > 0 && <span className="muted small">Refreshing…</span>}
      </div>

      {error && (
        <div className="error-box">
          {error}{" "}
          <button className="link-button" onClick={() => setReloadKey((v) => v + 1)}>Retry</button>
        </div>
      )}
      {loading && requests.length === 0 && <div className="empty">Loading…</div>}

      {(!loading || requests.length > 0) && !error && (
        <>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th className="clickable" onClick={() => toggleSort("started_at")}>Time{arrow("started_at")}</th>
                  <th className="clickable" onClick={() => toggleSort("id")}>Request{arrow("id")}</th>
                  <th>Session</th>
                  <th>Provider / model</th>
                  <th>Harness</th>
                  <th>HTTP</th>
                  <th className="clickable" onClick={() => toggleSort("duration")}>Duration{arrow("duration")}</th>
                  <th className="clickable numeric" onClick={() => toggleSort("total_tokens")}>Tokens{arrow("total_tokens")}</th>
                  <th className="clickable numeric" onClick={() => toggleSort("cost_amount")}>Cost{arrow("cost_amount")}</th>
                </tr>
              </thead>
              <tbody>
                {requests.map((r) => (
                  <tr
                    key={r.id}
                    className="clickable-row"
                    tabIndex={0}
                    role="link"
                    onClick={() => onOpenRequest(r.id)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        onOpenRequest(r.id);
                      }
                    }}
                  >
                    <td className="nowrap" title={formatTime(r.started_at)}>{formatRelativeTime(r.started_at)}</td>
                    <td>
                      <div className="request-id">
                        <span className={statusClass(r.status_code)}>{r.status_code || "ERR"}</span>
                        <span>#{r.id}</span>
                        {r.streamed && <span className="chip">streamed</span>}
                        {r.incomplete && <span className="chip warn">incomplete</span>}
                        {r.has_tool_calls && <span className="chip">tools</span>}
                      </div>
                    </td>
                    <td>
                      {r.session_key ? (
                        <div className="session-cell">
                          <span className="session-key">{r.session_key}</span>
                          {r.session_id !== undefined && (
                            <button
                              className="link-button"
                              title="Open session"
                              onClick={(e) => {
                                e.stopPropagation();
                                onOpenSession(r.session_id!);
                              }}
                            >
                              open
                            </button>
                          )}
                        </div>
                      ) : (
                        <span className="muted">unassigned</span>
                      )}
                    </td>
                    <td>
                      <div>{r.provider || <span className="muted">unknown provider</span>}</div>
                      {r.model && <div className="muted small">{r.model}</div>}
                    </td>
                    <td>{r.harness ? <span className="chip">{r.harness}</span> : <span className="muted">—</span>}</td>
                    <td className="path-cell">
                      <div>{r.method}</div>
                      <div className="muted small">{r.local_path}</div>
                    </td>
                    <td className="nowrap">{formatDuration(r.started_at, r.completed_at)}</td>
                    <td className="numeric token-cell">
                      <div>{formatTokens(r.total_tokens)}</div>
                      {(r.input_tokens !== undefined ||
                        r.output_tokens !== undefined ||
                        r.cached_input_tokens !== undefined ||
                        r.reasoning_tokens !== undefined) && (
                        <TokenBreakdown
                          input={r.input_tokens}
                          cached={r.cached_input_tokens}
                          generated={r.output_tokens}
                          reasoning={r.reasoning_tokens}
                          compact
                        />
                      )}
                    </td>
                    <td className="numeric">{formatCost(r.cost_amount)}</td>
                  </tr>
                ))}
                {requests.length === 0 && (
                  <tr>
                    <td colSpan={9} className="empty-cell">
                      No requests match these filters.
                      {activeFilterCount > 0 && (
                        <> <button className="link-button" onClick={clearFilters}>Clear filters</button></>
                      )}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          <Pagination
            page={page}
            pageSize={pageSize}
            total={total}
            onPageChange={setPage}
            onPageSizeChange={changePageSize}
          />
        </>
      )}
    </main>
  );
}
