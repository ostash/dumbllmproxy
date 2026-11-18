// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useState } from "react";
import { CopyButton } from "./CopyButton";

type Props = {
  value?: string;
};

type ExpandMode = "default" | "expanded" | "collapsed";

export function JSONView({ value }: Props) {
  const [expandMode, setExpandMode] = useState<ExpandMode>("default");
  const [revision, setRevision] = useState(0);
  function changeMode(mode: ExpandMode) {
    setExpandMode(mode);
    setRevision((v) => v + 1);
  }

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
        <div className="segmented" role="group" aria-label="JSON expansion">
          <button
            className={expandMode === "default" ? "active" : ""}
            onClick={() => changeMode("default")}
          >
            Default
          </button>
          <button
            className={expandMode === "expanded" ? "active" : ""}
            onClick={() => changeMode("expanded")}
          >
            Expand all
          </button>
          <button
            className={expandMode === "collapsed" ? "active" : ""}
            onClick={() => changeMode("collapsed")}
          >
            Collapse all
          </button>
        </div>
      </div>
      <div className="json-view" key={revision}>
        <JSONNode value={parsed} depth={0} mode={expandMode} />
      </div>
    </div>
  );
}

function JSONNode({ value, depth, mode }: { value: unknown; depth: number; mode: ExpandMode }) {
  if (Array.isArray(value)) {
    return (
      <JSONCollection
        kind="array"
        entries={value.map((item, index) => [String(index), item])}
        depth={depth}
        mode={mode}
      />
    );
  }
  if (value !== null && typeof value === "object") {
    return (
      <JSONCollection
        kind="object"
        entries={Object.entries(value as Record<string, unknown>)}
        depth={depth}
        mode={mode}
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
  mode
}: {
  kind: "array" | "object";
  entries: [string, unknown][];
  depth: number;
  mode: ExpandMode;
}) {
  const open = kind === "array" ? "[" : "{";
  const close = kind === "array" ? "]" : "}";
  if (entries.length === 0) {
    return <span className="json-punct">{open}{close}</span>;
  }
  return (
    <details className="json-node" open={mode === "expanded" || (mode === "default" && depth < 3)}>
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
            <JSONNode value={item} depth={depth + 1} mode={mode} />
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
