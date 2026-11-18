// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useState } from "react";

type Props = {
  text: string;
  maxLines: number;
  maxChars: number;
  className?: string;
};

// CollapsibleText shows a line-based preview of long conversation material and
// lets the reader expand it in place. maxChars is a safety cap for a single
// pathologically long line; nothing is silently dropped.
export function CollapsibleText({ text, maxLines, maxChars, className }: Props) {
  const [expanded, setExpanded] = useState(false);
  const lines = text.split("\n");
  const lineTruncated = lines.length > maxLines;
  const charTruncated = text.length > maxChars;
  const truncated = lineTruncated || charTruncated;
  let preview = text;
  if (truncated) {
    preview = lines.slice(0, maxLines).join("\n");
    if (preview.length > maxChars) {
      preview = preview.slice(0, maxChars);
    }
  }
  const shown = expanded || !truncated ? text : `${preview}\n…`;
  const countLabel = lineTruncated
    ? `${lines.length.toLocaleString()} lines`
    : `${text.length.toLocaleString()} chars`;
  return (
    <>
      <pre className={className}>{shown}</pre>
      {truncated && (
        <button
          className="link-button"
          onClick={(e) => {
            e.stopPropagation();
            setExpanded((v) => !v);
          }}
        >
          {expanded ? "show less" : `show full (${countLabel})`}
        </button>
      )}
    </>
  );
}
