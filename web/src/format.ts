// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

export function formatTime(value?: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

export function formatTokens(value?: number): string {
  if (value === undefined || value === null) return "—";
  return value.toLocaleString();
}

export function formatRelativeTime(value?: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const diff = date.getTime() - Date.now();
  const abs = Math.abs(diff);
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ["year", 365 * 24 * 60 * 60 * 1000],
    ["month", 30 * 24 * 60 * 60 * 1000],
    ["week", 7 * 24 * 60 * 60 * 1000],
    ["day", 24 * 60 * 60 * 1000],
    ["hour", 60 * 60 * 1000],
    ["minute", 60 * 1000],
    ["second", 1000]
  ];
  const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  for (const [unit, ms] of units) {
    if (abs >= ms || unit === "second") {
      return formatter.format(Math.round(diff / ms), unit);
    }
  }
  return "now";
}

export function formatDuration(started?: string, completed?: string): string {
  if (!started || !completed) return "—";
  const start = new Date(started).getTime();
  const end = new Date(completed).getTime();
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return "—";
  const ms = end - start;
  if (ms < 1000) return `${ms} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`;
  const minutes = Math.floor(ms / 60_000);
  const seconds = Math.round((ms % 60_000) / 1000);
  if (minutes < 60) return `${minutes}m ${seconds}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
}

export function nonCachedInputTokens(input?: number, cached?: number): number | undefined {
  if (input === undefined || input === null || cached === undefined || cached === null) {
    return undefined;
  }
  return Math.max(0, input - cached);
}

export function formatCost(amount?: number): string {
  if (amount === undefined || amount === null) return "—";
  const abs = Math.abs(amount);
  const tiny = abs > 0 && abs < 0.01;
  try {
    return new Intl.NumberFormat(undefined, {
      style: "currency",
      currency: "USD",
      // Do not round very small costs to a handful of decimals; provider
      // costs can legitimately carry more precision than that.
      minimumFractionDigits: tiny ? 0 : 2,
      maximumFractionDigits: tiny ? 20 : 6
    }).format(amount);
  } catch {
    return `${amount} USD`;
  }
}

export function prettyJSON(value: unknown): string {
  if (value === undefined || value === null) return "—";
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}


export function statusClass(status: number): string {
  if (status >= 200 && status < 300) return "status ok";
  if (status >= 300 && status < 400) return "status redirect";
  if (status >= 400 && status < 500) return "status client-error";
  return "status server-error";
}
