// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useEffect, useState } from "react";
import { api } from "./api";
import type { Facets, RequestSummary } from "./types";
import {
  formatCost,
  formatDuration,
  formatTime,
  formatTimestamp,
  formatTokens
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

function requestRowClass(r: RequestSummary): string {
  if (r.status_code < 200 || r.status_code >= 400) return "clickable-row row-error";
  if (r.status_code >= 300) return "clickable-row row-redirect";
  if (r.incomplete) return "clickable-row row-incomplete";
  return "clickable-row";
}

function isNonSuccess(status: number): boolean {
  return status < 200 || status >= 300;
}

function statusChipClass(status: number): string {
  return status < 200 || status >= 400 ? "error" : "redirect";
}

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
  const [toolCalls, setToolCalls] = useState(() => boolParam(initialParams, "tool_calls"));
  const [unassigned, setUnassigned] = useState(() => boolParam(initialParams, "unassigned"));
  const [provider, setProvider] = useState(() => stringParam(initialParams, "provider"));
  const [model, setModel] = useState(() => stringParam(initialParams, "model"));
  const [harness, setHarness] = useState(() => stringParam(initialParams, "harness"));
  const [selectedIds, setSelectedIds] = useState<Set<number>>(() => new Set());
  const [deleteTargets, setDeleteTargets] = useState<number[] | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const debouncedFilter = useDebouncedValue(filter);
  const debouncedProvider = useDebouncedValue(provider);
  const debouncedModel = useDebouncedValue(model);
  const debouncedHarness = useDebouncedValue(harness);
  const filtersPending =
    filter !== debouncedFilter ||
    provider !== debouncedProvider ||
    model !== debouncedModel ||
    harness !== debouncedHarness;

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
    if (filtersPending) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    const since = sinceForWindow(timeWindow);
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
        tool_calls: toolCalls || undefined,
        unassigned: unassigned || undefined,
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
    filtersPending, refreshKey, reloadKey, page, pageSize, timeWindow, sortKey, sortDir,
    debouncedFilter, failed, incomplete, toolCalls, unassigned,
    debouncedProvider, debouncedModel, debouncedHarness
  ]);

  useEffect(() => {
    setSelectedIds(new Set());
    setDeleteTargets(null);
    setDeleteError("");
  }, [
    page, pageSize, timeWindow, sortKey, sortDir, debouncedFilter, failed,
    incomplete, toolCalls, unassigned, debouncedProvider, debouncedModel,
    debouncedHarness
  ]);

  useEffect(() => {
    setSelectedIds((prev) => {
      if (prev.size === 0) return prev;
      const present = new Set(requests.map((request) => request.id));
      let changed = false;
      const next = new Set<number>();
      for (const id of prev) {
        if (present.has(id)) next.add(id);
        else changed = true;
      }
      return changed ? next : prev;
    });
  }, [requests]);

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
    if (toolCalls) params.set("tool_calls", "1");
    if (unassigned) params.set("unassigned", "1");
    if (provider.trim()) params.set("provider", provider.trim());
    if (model.trim()) params.set("model", model.trim());
    if (harness.trim()) params.set("harness", harness.trim());
    const query = params.toString();
    const hash = query ? `#/requests?${query}` : "#/requests";
    window.history.replaceState(null, "", hash);
    onListHash(hash);
  }, [
    page, pageSize, timeWindow, sortKey, sortDir, filter,
    failed, incomplete, toolCalls, unassigned,
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
    filter.trim(), failed, incomplete, toolCalls, unassigned,
    provider.trim(), model.trim(), harness.trim()
  ].filter(Boolean).length;

  function clearFilters() {
    setFilter("");
    setFailed(false);
    setIncomplete(false);
    setToolCalls(false);
    setUnassigned(false);
    setProvider("");
    setModel("");
    setHarness("");
    setPage(0);
  }

  const pageIds = requests.map((request) => request.id);
  const allSelected = pageIds.length > 0 && pageIds.every((id) => selectedIds.has(id));
  const someSelected = pageIds.some((id) => selectedIds.has(id));

  function toggleAll() {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (allSelected) {
        for (const id of pageIds) next.delete(id);
      } else {
        for (const id of pageIds) next.add(id);
      }
      return next;
    });
  }

  function toggleSelected(id: number) {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function clearSelection() {
    setSelectedIds(new Set());
  }

  async function confirmDelete() {
    if (!deleteTargets?.length) return;
    setDeleting(true);
    setDeleteError("");
    try {
      await api.deleteRequests(deleteTargets);
      setDeleteTargets(null);
      setSelectedIds(new Set());
      setReloadKey((v) => v + 1);
    } catch (e) {
      setDeleteError(String(e));
    } finally {
      setDeleting(false);
    }
  }

  function cancelDelete() {
    if (deleting) return;
    setDeleteTargets(null);
    setDeleteError("");
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
            placeholder="Search ID, model, endpoint, session…"
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
        <button className={`filter-chip ${toolCalls ? "active" : ""}`} aria-pressed={toolCalls} onClick={() => toggleFilter(setToolCalls)}>Tool calls</button>
        <button className={`filter-chip ${unassigned ? "active" : ""}`} aria-pressed={unassigned} onClick={() => toggleFilter(setUnassigned)} title="Conversation requests without a session">Unassigned</button>
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

      {selectedIds.size > 0 && (
        <div className="selection-bar">
          <span>{selectedIds.size} selected</span>
          <button className="danger" onClick={() => setDeleteTargets([...selectedIds])}>
            Delete selected
          </button>
          <button className="secondary" onClick={clearSelection}>Clear</button>
        </div>
      )}
      {deleteTargets && (
        <div className="danger-box">
          <span>
            {deleteTargets.length === 1
              ? `Delete request #${deleteTargets[0]}?`
              : `Delete ${deleteTargets.length} requests?`}{" "}
            This cannot be undone.
          </span>
          <div className="danger-actions">
            <button className="danger" onClick={confirmDelete} disabled={deleting}>
              {deleting ? "Deleting…" : "Delete"}
            </button>
            <button className="secondary" onClick={cancelDelete} disabled={deleting}>
              Cancel
            </button>
          </div>
        </div>
      )}
      {deleteError && <div className="error-box">{deleteError}</div>}

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
                  <th className="checkbox-cell">
                    <input
                      type="checkbox"
                      aria-label="Select all requests on this page"
                      checked={allSelected}
                      ref={(el) => {
                        if (el) el.indeterminate = someSelected && !allSelected;
                      }}
                      onChange={toggleAll}
                      onClick={(e) => e.stopPropagation()}
                    />
                  </th>
                  <th className="clickable" onClick={() => toggleSort("id")}>ID{arrow("id")}</th>
                  <th className="clickable" onClick={() => toggleSort("started_at")}>Time{arrow("started_at")}</th>
                  <th>Session</th>
                  <th>Provider / model</th>
                  <th>Harness</th>
                  <th>HTTP</th>
                  <th className="clickable" onClick={() => toggleSort("duration")}>Duration{arrow("duration")}</th>
                  <th className="clickable numeric" onClick={() => toggleSort("total_tokens")}>Tokens{arrow("total_tokens")}</th>
                  <th className="clickable numeric" onClick={() => toggleSort("cost_amount")}>Cost{arrow("cost_amount")}</th>
                  <th className="actions-cell" aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {requests.map((r) => (
                  <tr
                    key={r.id}
                    className={requestRowClass(r)}
                    tabIndex={0}
                    role="link"
                    onClick={() => onOpenRequest(r.id)}
                    onKeyDown={(e) => {
                      if (e.target !== e.currentTarget) return;
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        onOpenRequest(r.id);
                      }
                    }}
                  >
                    <td className="checkbox-cell" onClick={(e) => e.stopPropagation()}>
                      <input
                        type="checkbox"
                        aria-label={`Select request ${r.id}`}
                        checked={selectedIds.has(r.id)}
                        onChange={() => toggleSelected(r.id)}
                        onClick={(e) => e.stopPropagation()}
                      />
                    </td>
                    <td className="request-number" title={`HTTP ${r.status_code || "ERR"}`}>
                      #{r.id}
                      {isNonSuccess(r.status_code) && (
                        <span className={`status-chip ${statusChipClass(r.status_code)}`}>
                          {r.status_code || "ERR"}
                        </span>
                      )}
                    </td>
                    <td className="nowrap" title={formatTime(r.started_at)}>{formatTimestamp(r.started_at)}</td>
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
                      <div className="muted small">{[r.provider, r.endpoint].filter(Boolean).join(" ")}</div>
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
                    <td className="actions-cell" onClick={(e) => e.stopPropagation()}>
                      <button
                        className="link-button danger-text"
                        onClick={(e) => {
                          e.stopPropagation();
                          setDeleteTargets([r.id]);
                        }}
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
                {requests.length === 0 && (
                  <tr>
                    <td colSpan={11} className="empty-cell">
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
