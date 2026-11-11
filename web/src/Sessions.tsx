// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useEffect, useMemo, useState } from "react";
import { api } from "./api";
import type { Facets, SessionSummary } from "./types";
import { formatCost, formatRelativeTime, formatTime, formatTokens } from "./format";
import { TokenBreakdown } from "./TokenBreakdown";
import { PartialChip, hasNoData } from "./Completeness";
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

const SORT_KEYS = ["session_key", "last_activity", "request_count", "total_tokens", "cost_amount"] as const;
type SortKey = (typeof SORT_KEYS)[number];

type Props = {
  initialParams: URLSearchParams;
  refreshKey: number;
  onSelect: (id: number) => void;
  onListHash: (hash: string) => void;
};

export function Sessions({ initialParams, refreshKey, onSelect, onListHash }: Props) {
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [facets, setFacets] = useState<Facets>({ providers: [], models: [], harnesses: [] });
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [reloadKey, setReloadKey] = useState(0);
  const [filter, setFilter] = useState(() => stringParam(initialParams, "q"));
  const [sortKey, setSortKey] = useState<SortKey>(() => sortParam(initialParams, SORT_KEYS, "last_activity"));
  const [sortDir, setSortDir] = useState<"asc" | "desc">(() => orderParam(initialParams));
  const [page, setPage] = useState(() => intParam(initialParams, "page", 0));
  const [pageSize, setPageSize] = useState<PageSize>(() => pageSizeParam(initialParams));
  const [timeWindow, setTimeWindow] = useState<TimeWindow>(() => timeWindowParam(initialParams));
  const [unassigned, setUnassigned] = useState(() => boolParam(initialParams, "unassigned"));
  const [failed, setFailed] = useState(() => boolParam(initialParams, "has_failures"));
  const [incomplete, setIncomplete] = useState(() => boolParam(initialParams, "has_incomplete"));
  const [provider, setProvider] = useState(() => stringParam(initialParams, "provider"));
  const [model, setModel] = useState(() => stringParam(initialParams, "model"));
  const since = useMemo(() => sinceForWindow(timeWindow), [timeWindow]);
  const debouncedFilter = useDebouncedValue(filter);
  const debouncedProvider = useDebouncedValue(provider);
  const debouncedModel = useDebouncedValue(model);

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
      .sessions({
        limit: pageSize,
        offset: page * pageSize,
        since,
        sort: sortKey,
        order: sortDir,
        q: debouncedFilter.trim() || undefined,
        unassigned: unassigned || undefined,
        has_failures: failed || undefined,
        has_incomplete: incomplete || undefined,
        provider: debouncedProvider.trim() || undefined,
        model: debouncedModel.trim() || undefined
      })
      .then((r) => {
        if (!cancelled) {
          setSessions(r.sessions);
          setTotal(r.total);
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
    debouncedFilter, unassigned, failed, incomplete, debouncedProvider, debouncedModel
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
    if (sortKey !== "last_activity") params.set("sort", sortKey);
    if (sortDir !== "desc") params.set("order", sortDir);
    if (filter.trim()) params.set("q", filter.trim());
    if (unassigned) params.set("unassigned", "1");
    if (failed) params.set("has_failures", "1");
    if (incomplete) params.set("has_incomplete", "1");
    if (provider.trim()) params.set("provider", provider.trim());
    if (model.trim()) params.set("model", model.trim());
    const query = params.toString();
    const hash = query ? `#/?${query}` : "#/";
    window.history.replaceState(null, "", hash);
    onListHash(hash);
  }, [
    page, pageSize, timeWindow, sortKey, sortDir, filter,
    unassigned, failed, incomplete, provider, model, onListHash
  ]);

  function toggleSort(key: SortKey) {
    if (key === sortKey) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir(key === "session_key" ? "asc" : "desc");
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

  const arrow = (key: SortKey) => (sortKey === key ? (sortDir === "asc" ? " ▲" : " ▼") : "");
  const activeFilterCount = [
    filter.trim(), unassigned, failed, incomplete, provider.trim(), model.trim()
  ].filter(Boolean).length;

  function clearFilters() {
    setFilter("");
    setUnassigned(false);
    setFailed(false);
    setIncomplete(false);
    setProvider("");
    setModel("");
    setPage(0);
  }

  return (
    <main className="page">
      <div className="page-heading">
        <div>
          <h1>Sessions</h1>
          <p className="muted">Captured LLM conversations grouped by configured session headers.</p>
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
            aria-label="Search sessions"
            placeholder="Search all sessions…"
            value={filter}
            onChange={(e) => {
              setFilter(e.target.value);
              setPage(0);
            }}
          />
        </div>
      </div>

      <div className="quick-filters">
        <button
          className={`filter-chip ${unassigned ? "active" : ""}`}
          aria-pressed={unassigned}
          onClick={() => {
            setUnassigned((v) => !v);
            setPage(0);
          }}
        >
          Unassigned
        </button>
        <button
          className={`filter-chip ${failed ? "active" : ""}`}
          aria-pressed={failed}
          onClick={() => {
            setFailed((v) => !v);
            setPage(0);
          }}
        >
          Failed
        </button>
        <button
          className={`filter-chip ${incomplete ? "active" : ""}`}
          aria-pressed={incomplete}
          onClick={() => {
            setIncomplete((v) => !v);
            setPage(0);
          }}
        >
          Incomplete
        </button>
        <input
          className="filter small-filter"
          aria-label="Provider filter"
          placeholder="Provider"
          list="session-provider-options"
          value={provider}
          onChange={(e) => {
            setProvider(e.target.value);
            setPage(0);
          }}
        />
        <datalist id="session-provider-options">
          {facets.providers.map((value) => <option key={value} value={value} />)}
        </datalist>
        <input
          className="filter small-filter"
          aria-label="Model filter"
          placeholder="Model"
          list="session-model-options"
          value={model}
          onChange={(e) => {
            setModel(e.target.value);
            setPage(0);
          }}
        />
        <datalist id="session-model-options">
          {facets.models.map((value) => <option key={value} value={value} />)}
        </datalist>
        {activeFilterCount > 0 && (
          <button className="filter-chip clear-chip" onClick={clearFilters}>
            Clear {activeFilterCount} filter{activeFilterCount === 1 ? "" : "s"}
          </button>
        )}
        {loading && sessions.length > 0 && <span className="muted small">Refreshing…</span>}
      </div>

      {error && (
        <div className="error-box">
          {error}{" "}
          <button className="link-button" onClick={() => setReloadKey((v) => v + 1)}>Retry</button>
        </div>
      )}
      {loading && sessions.length === 0 && <div className="empty">Loading…</div>}

      {(!loading || sessions.length > 0) && !error && (
        <>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th className="clickable" onClick={() => toggleSort("session_key")}>Session{arrow("session_key")}</th>
                  <th>Start</th>
                  <th className="clickable" onClick={() => toggleSort("last_activity")}>Last activity{arrow("last_activity")}</th>
                  <th className="clickable numeric" onClick={() => toggleSort("request_count")}>Requests{arrow("request_count")}</th>
                  <th>Provider / model</th>
                  <th className="clickable numeric" onClick={() => toggleSort("total_tokens")}>Tokens{arrow("total_tokens")}</th>
                  <th className="clickable numeric" onClick={() => toggleSort("cost_amount")}>Cost{arrow("cost_amount")}</th>
                </tr>
              </thead>
              <tbody>
                {sessions.map((s) => (
                  <tr
                    key={s.unassigned ? "unassigned" : s.id}
                    className="clickable-row"
                    tabIndex={0}
                    role="link"
                    onClick={() => onSelect(s.id)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        onSelect(s.id);
                      }
                    }}
                  >
                    <td>
                      <div className="session-key">{s.session_key}</div>
                      {s.unassigned && <span className="chip muted-chip">unassigned</span>}
                      {s.failed_requests > 0 && <span className="chip warn">{s.failed_requests} failed</span>}
                      {s.incomplete_requests > 0 && <span className="chip warn">{s.incomplete_requests} incomplete</span>}
                    </td>
                    <td className="nowrap" title={formatTime(s.first_seen)}>{formatRelativeTime(s.first_seen)}</td>
                    <td className="nowrap" title={formatTime(s.last_activity)}>{formatRelativeTime(s.last_activity)}</td>
                    <td className="numeric">{s.request_count}</td>
                    <td>
                      {s.providers?.length ? <div>{s.providers.join(", ")}</div> : null}
                      {s.models?.length ? <div className="muted small">{s.models.join(", ")}</div> : null}
                      {!s.providers?.length && !s.models?.length ? <span className="muted">—</span> : null}
                    </td>
                    <td className="numeric token-cell">
                      <div>
                        {hasNoData(s.usage_reported_requests) ? "—" : formatTokens(s.total_tokens)}
                        <PartialChip reported={s.usage_reported_requests} total={s.request_count} label="usage" />
                      </div>
                      {!hasNoData(s.usage_reported_requests) &&
                        (s.total_tokens > 0 || s.cached_input_tokens !== undefined || s.reasoning_tokens !== undefined) && (
                          <TokenBreakdown
                            input={s.input_tokens}
                            cached={s.cached_input_tokens}
                            generated={s.output_tokens}
                            reasoning={s.reasoning_tokens}
                            compact
                          />
                        )}
                    </td>
                    <td className="numeric">
                      {hasNoData(s.cost_reported_requests) ? "—" : formatCost(s.cost_amount)}
                      <PartialChip reported={s.cost_reported_requests} total={s.request_count} label="cost" />
                    </td>
                  </tr>
                ))}
                {sessions.length === 0 && (
                  <tr>
                    <td colSpan={7} className="empty-cell">
                      No sessions match these filters.
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
