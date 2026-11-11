// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useState } from "react";
import { CopyButton } from "./CopyButton";

type Props = {
  value?: string;
};

export function JSONView({ value }: Props) {
  const [expandAll, setExpandAll] = useState(false);

  if (!value) {
    return (
      <div className="payload-frame">
        <CopyButton text={value} />
        <pre className="payload">—</pre>
      </div>
    );
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(value);
  } catch {
    return (
      <div className="payload-frame">
        <CopyButton text={value} />
        <pre className="payload">{value}</pre>
      </div>
    );
  }
  const copyText = JSON.stringify(parsed, null, 2);
  return (
    <div className="payload-frame">
      <div className="payload-toolbar">
        <CopyButton text={copyText} />
        <button className="link-button" onClick={() => setExpandAll((v) => !v)}>
          {expandAll ? "Collapse all" : "Expand all"}
        </button>
      </div>
      <div className="json-view">
        <JSONNode value={parsed} depth={0} expandAll={expandAll} />
      </div>
    </div>
  );
}

function JSONNode({ value, depth, expandAll }: { value: unknown; depth: number; expandAll: boolean }) {
  if (Array.isArray(value)) {
    return (
      <JSONCollection
        kind="array"
        entries={value.map((item, index) => [String(index), item])}
        depth={depth}
        expandAll={expandAll}
      />
    );
  }
  if (value !== null && typeof value === "object") {
    return (
      <JSONCollection
        kind="object"
        entries={Object.entries(value as Record<string, unknown>)}
        depth={depth}
        expandAll={expandAll}
      />
    );
  }
  const primitive = primitiveView(value);
  return <span className={`json-${primitive.kind}`}>{primitive.text}</span>;
}

function JSONCollection({
  kind,
  entries,
  depth,
  expandAll
}: {
  kind: "array" | "object";
  entries: [string, unknown][];
  depth: number;
  expandAll: boolean;
}) {
  const open = kind === "array" ? "[" : "{";
  const close = kind === "array" ? "]" : "}";
  if (entries.length === 0) {
    return <span className="json-punct">{open}{close}</span>;
  }
  return (
    <details className="json-node" open={expandAll || depth < 3}>
      <summary>
        <span className="json-punct">{open}</span>
        <span className="json-summary">…</span>
        <span className="json-punct">{close}</span>
        <span className="json-count">
          {entries.length} {kind === "array" ? "items" : "keys"}
        </span>
      </summary>
      <div className="json-children">
        {entries.map(([key, item], index) => (
          <div className="json-entry" key={key}>
            {kind === "object" && (
              <>
                <span className="json-key">{JSON.stringify(key)}</span>
                <span className="json-punct">: </span>
              </>
            )}
            <JSONNode value={item} depth={depth + 1} expandAll={expandAll} />
            {index < entries.length - 1 && <span className="json-punct">,</span>}
          </div>
        ))}
      </div>
    </details>
  );
}

function primitiveView(value: unknown): { text: string; kind: string } {
  if (value === null) {
    return { text: "null", kind: "null" };
  }
  switch (typeof value) {
    case "string":
      return { text: JSON.stringify(value), kind: "string" };
    case "number":
      return { text: Number.isFinite(value) ? String(value) : "null", kind: "number" };
    case "boolean":
      return { text: String(value), kind: "boolean" };
    default:
      return { text: JSON.stringify(value) ?? String(value), kind: "null" };
  }
}
