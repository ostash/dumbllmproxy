// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useState } from "react";

type Props = {
  text: string;
  limit: number;
  className?: string;
};

// CollapsibleText shows a compact preview of long conversation material and
// lets the reader expand it in place. Nothing is silently dropped.
export function CollapsibleText({ text, limit, className }: Props) {
  const [expanded, setExpanded] = useState(false);
  const truncated = text.length > limit;
  const shown = expanded || !truncated ? text : text.slice(0, limit) + "…";
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
          {expanded ? "show less" : `show full (${text.length.toLocaleString()} chars)`}
        </button>
      )}
    </>
  );
}
