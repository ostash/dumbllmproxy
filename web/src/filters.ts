// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

export type TimeWindow = "all" | "1h" | "3h" | "6h" | "12h" | "1d" | "2d" | "1w" | "1mo" | "1y";

export const TIME_WINDOW_OPTIONS: { value: TimeWindow; label: string }[] = [
  { value: "all", label: "All time" },
  { value: "1h", label: "Last 1 hour" },
  { value: "3h", label: "Last 3 hours" },
  { value: "6h", label: "Last 6 hours" },
  { value: "12h", label: "Last 12 hours" },
  { value: "1d", label: "Last 1 day" },
  { value: "2d", label: "Last 2 days" },
  { value: "1w", label: "Last 1 week" },
  { value: "1mo", label: "Last 1 month" },
  { value: "1y", label: "Last 1 year" }
];

export const PAGE_SIZES = [50, 100, 200] as const;
export type PageSize = (typeof PAGE_SIZES)[number];

export function sinceForWindow(value: TimeWindow): string | undefined {
  if (value === "all") return undefined;
  const date = new Date();
  switch (value) {
    case "1h":
      date.setHours(date.getHours() - 1);
      break;
    case "3h":
      date.setHours(date.getHours() - 3);
      break;
    case "6h":
      date.setHours(date.getHours() - 6);
      break;
    case "12h":
      date.setHours(date.getHours() - 12);
      break;
    case "1d":
      date.setDate(date.getDate() - 1);
      break;
    case "2d":
      date.setDate(date.getDate() - 2);
      break;
    case "1w":
      date.setDate(date.getDate() - 7);
      break;
    case "1mo":
      date.setMonth(date.getMonth() - 1);
      break;
    case "1y":
      date.setFullYear(date.getFullYear() - 1);
      break;
  }
  return date.toISOString();
}
