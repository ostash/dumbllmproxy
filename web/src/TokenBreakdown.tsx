// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { formatTokens, nonCachedInputTokens } from "./format";

type Props = {
  input?: number;
  cached?: number;
  generated?: number;
  reasoning?: number;
  compact?: boolean;
  className?: string;
};

type TokenItem = {
  key: string;
  label: string;
  value?: number;
  title?: string;
};

export function TokenBreakdown({
  input,
  cached,
  generated,
  reasoning,
  compact = false,
  className = ""
}: Props) {
  const items: TokenItem[] = [
    { key: "input", label: compact ? "in" : "Input", value: input },
    { key: "cached", label: compact ? "cached" : "Cached input", value: cached },
    {
      key: "non-cached",
      label: compact ? "non-cached" : "Non-cached input",
      value: nonCachedInputTokens(input, cached)
    },
    {
      key: "generated",
      label: compact ? "gen" : "Generated",
      value: generated,
      title: "Generated tokens include reasoning tokens when reported by the provider."
    },
    {
      key: "reasoning",
      label: compact ? "reasoning" : "Reasoning",
      value: reasoning,
      title: "Reasoning tokens are included in generated tokens when reported by the provider."
    }
  ];
  const available = items.filter((item) => item.value !== undefined && item.value !== null);
  if (available.length === 0) {
    return <span className={`muted ${className}`.trim()}>—</span>;
  }
  return (
    <span className={`token-breakdown ${className}`.trim()}>
      {available.map((item, index) => (
        <span className="token-item" key={item.key} title={item.title}>
          {index > 0 && <span className="token-sep">·</span>}
          <span className="token-label">{item.label}</span>{" "}
          <span className="token-value">{formatTokens(item.value)}</span>
        </span>
      ))}
    </span>
  );
}
