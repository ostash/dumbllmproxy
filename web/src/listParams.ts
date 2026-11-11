// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { PAGE_SIZES, TIME_WINDOW_OPTIONS, type PageSize, type TimeWindow } from "./filters";

export function intParam(params: URLSearchParams, name: string, fallback: number): number {
  const raw = params.get(name);
  if (raw === null) return fallback;
  const value = Number(raw);
  return Number.isFinite(value) && value >= 0 ? Math.floor(value) : fallback;
}

export function pageSizeParam(params: URLSearchParams): PageSize {
  const value = Number(params.get("page_size"));
  return (PAGE_SIZES as readonly number[]).includes(value) ? (value as PageSize) : 50;
}

export function timeWindowParam(params: URLSearchParams): TimeWindow {
  const value = params.get("window");
  return TIME_WINDOW_OPTIONS.some((option) => option.value === value)
    ? (value as TimeWindow)
    : "all";
}

export function boolParam(params: URLSearchParams, name: string): boolean {
  return params.get(name) === "1";
}

export function stringParam(params: URLSearchParams, name: string): string {
  return params.get(name) ?? "";
}

export function sortParam<T extends string>(
  params: URLSearchParams,
  allowed: readonly T[],
  fallback: T
): T {
  const value = params.get("sort");
  return allowed.includes(value as T) ? (value as T) : fallback;
}

export function orderParam(params: URLSearchParams): "asc" | "desc" {
  return params.get("order") === "asc" ? "asc" : "desc";
}
