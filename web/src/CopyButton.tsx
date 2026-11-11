// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useEffect, useRef, useState, type MouseEvent } from "react";

type Props = {
  text?: string;
  className?: string;
};

export function CopyButton({ text, className = "" }: Props) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number | undefined>(undefined);

  useEffect(() => {
    return () => {
      if (timer.current !== undefined) window.clearTimeout(timer.current);
    };
  }, []);

  async function copy(event: MouseEvent<HTMLButtonElement>) {
    event.stopPropagation();
    if (!text) return;
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
      } else {
        fallbackCopy(text);
      }
      setCopied(true);
      if (timer.current !== undefined) window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setCopied(false), 1500);
    } catch {
      fallbackCopy(text);
      setCopied(true);
      if (timer.current !== undefined) window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setCopied(false), 1500);
    }
  }

  return (
    <button
      type="button"
      className={`copy-button ${className}`.trim()}
      onClick={copy}
      disabled={!text}
      title={copied ? "Copied" : "Copy to clipboard"}
    >
      {copied ? "Copied" : "Copy"}
    </button>
  );
}

function fallbackCopy(text: string) {
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();
  document.execCommand("copy");
  document.body.removeChild(textarea);
}
