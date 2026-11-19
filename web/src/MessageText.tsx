// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import { CollapsibleText } from "./CollapsibleText";

type Props = {
  text: string;
  maxLines: number;
  maxChars: number;
  className?: string;
  defaultFormatted?: boolean;
  formatted?: boolean;
  onToggleFormatted?: () => void;
  showToolbar?: boolean;
  full?: boolean;
};

export function MessageText({
  text,
  maxLines,
  maxChars,
  className,
  defaultFormatted = false,
  formatted: controlledFormatted,
  onToggleFormatted,
  showToolbar = true,
  full = false
}: Props) {
  const [internalFormatted, setInternalFormatted] = useState(defaultFormatted);
  const [expanded, setExpanded] = useState(false);
  const formatted = controlledFormatted ?? internalFormatted;
  const toggleFormatted = onToggleFormatted ?? (() => setInternalFormatted((v) => !v));
  const lines = text.split("\n");
  const long = !full && (lines.length > maxLines || text.length > maxChars);

  return (
    <div className="message-text">
      {showToolbar && (
        <div className="message-text-toolbar">
          <button
            className="link-button"
            onClick={(e) => {
              e.stopPropagation();
              toggleFormatted();
            }}
          >
            {formatted ? "show raw" : "show formatted"}
          </button>
        </div>
      )}
      {formatted ? (
        <>
          <div className={`markdown${long && !expanded ? " markdown-collapsed" : ""}`}>
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              rehypePlugins={[[rehypeHighlight, { detect: false }]]}
              components={{
                a: ({ node: _node, ...props }) => (
                  <a
                    {...props}
                    target="_blank"
                    rel="noopener noreferrer"
                    onClick={(e) => e.stopPropagation()}
                  />
                ),
                img: ({ node: _node, alt }) => (
                  <span className="markdown-image">[image{alt ? `: ${alt}` : ""}]</span>
                )
              }}
            >
              {text}
            </ReactMarkdown>
          </div>
          {long && (
            <button
              className="link-button"
              onClick={(e) => {
                e.stopPropagation();
                setExpanded((v) => !v);
              }}
            >
              {expanded ? "show less" : "show full"}
            </button>
          )}
        </>
      ) : full ? (
        <pre className={className}>{text}</pre>
      ) : (
        <CollapsibleText text={text} maxLines={maxLines} maxChars={maxChars} className={className} />
      )}
    </div>
  );
}
