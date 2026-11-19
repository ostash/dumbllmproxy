// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { Fragment } from "react";

type Props = {
  value?: string;
};

export function ExtractedHeaders({ value }: Props) {
  if (!value) {
    return <p className="muted small">No extracted headers.</p>;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(value);
  } catch {
    return <pre className="payload">{value}</pre>;
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return <pre className="payload">{value}</pre>;
  }
  const entries = Object.entries(parsed as Record<string, unknown>);
  if (entries.length === 0) {
    return <p className="muted small">No extracted headers.</p>;
  }
  return (
    <dl className="kv">
      {entries.map(([key, headerValue]) => (
        <Fragment key={key}>
          <dt>{key}</dt>
          <dd>{formatHeaderValue(headerValue)}</dd>
        </Fragment>
      ))}
    </dl>
  );
}

function formatHeaderValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (value === null) return "null";
  return JSON.stringify(value) ?? String(value);
}
