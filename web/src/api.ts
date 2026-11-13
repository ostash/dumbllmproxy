// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import type { Facets, RequestDetail, RequestSummary, SessionDetail, SessionSummary } from "./types";

export type ListParams = {
  limit?: number;
  offset?: number;
  since?: string;
  sort?: string;
  order?: "asc" | "desc";
  q?: string;
  status?: "failed";
  incomplete?: boolean;
  tool_calls?: boolean;
  unassigned?: boolean;
  provider?: string;
  model?: string;
  harness?: string;
  has_failures?: boolean;
  has_incomplete?: boolean;
};

export type ListResult<T> = {
  total: number;
  limit: number;
  offset: number;
} & T;

async function getJSON<T>(url: string): Promise<T> {
  const res = await fetch(url);
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
    } catch {
      // ignore
    }
    throw new Error(message);
  }
  return (await res.json()) as T;
}

function withQuery(url: string, params: ListParams): string {
  const search = new URLSearchParams();
  if (params.limit !== undefined) search.set("limit", String(params.limit));
  if (params.offset !== undefined) search.set("offset", String(params.offset));
  if (params.since) search.set("since", params.since);
  if (params.sort) search.set("sort", params.sort);
  if (params.order) search.set("order", params.order);
  if (params.q) search.set("q", params.q);
  if (params.status) search.set("status", params.status);
  if (params.incomplete) search.set("incomplete", "1");
  if (params.tool_calls) search.set("tool_calls", "1");
  if (params.unassigned) search.set("unassigned", "1");
  if (params.provider) search.set("provider", params.provider);
  if (params.model) search.set("model", params.model);
  if (params.harness) search.set("harness", params.harness);
  if (params.has_failures) search.set("has_failures", "1");
  if (params.has_incomplete) search.set("has_incomplete", "1");
  const query = search.toString();
  return query ? `${url}?${query}` : url;
}

export const api = {
  sessions: (params: ListParams = {}) =>
    getJSON<ListResult<{ sessions: SessionSummary[] }>>(withQuery("/api/sessions", params)),
  session: (id: number) => getJSON<SessionDetail>(`/api/sessions/${id}`),
  requests: (params: ListParams = {}) =>
    getJSON<ListResult<{ requests: RequestSummary[] }>>(withQuery("/api/requests", params)),
  request: (id: number) => getJSON<RequestDetail>(`/api/requests/${id}`),
  facets: () => getJSON<Facets>("/api/facets")
};
