// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import type { RequestDetail } from "./types";
import {
  formatCost,
  formatDuration,
  formatTime,
  formatTokens,
  nonCachedInputTokens,
  statusClass
} from "./format";
import { JSONView } from "./JSONView";
import { PayloadView } from "./PayloadView";
import { CopyButton } from "./CopyButton";

type Props = {
  requestId: number;
  onClose: () => void;
  onOpenRequest?: (id: number) => void;
};

type Tab = "payload" | "http" | "usage";

export function RequestDrawer({ requestId, onClose, onOpenRequest }: Props) {
  const [detail, setDetail] = useState<RequestDetail | null>(null);
  const [error, setError] = useState("");
  const [tab, setTab] = useState<Tab>("payload");
  const asideRef = useRef<HTMLElement>(null);

  useEffect(() => {
    let cancelled = false;
    setDetail(null);
    setError("");
    setTab("payload");
    api
      .request(requestId)
      .then((d) => {
        if (!cancelled) setDetail(d);
      })
      .catch((e) => {
        if (!cancelled) setError(String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [requestId]);

  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const node = asideRef.current;
    node?.focus();
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        onClose();
        return;
      }
      if (e.key !== "Tab" || !node) return;
      const focusable = node.querySelectorAll<HTMLElement>(
        'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
      );
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      previous?.focus();
    };
  }, [onClose]);

  return (
    <div className="drawer-backdrop" onClick={onClose}>
      <aside
        className="drawer"
        role="dialog"
        aria-modal="true"
        aria-labelledby="drawer-title"
        tabIndex={-1}
        ref={asideRef}
        onClick={(e) => e.stopPropagation()}
      >
        <header className="drawer-head">
          <div>
            <h2 id="drawer-title">HTTP request #{requestId}</h2>
            {detail && (
              <div className="muted small">
                {detail.method} {detail.local_path} · {formatTime(detail.started_at)}
              </div>
            )}
          </div>
          <div className="drawer-actions">
            <CopyButton text={String(requestId)} />
            {onOpenRequest && (
              <button className="secondary" onClick={() => onOpenRequest(requestId)}>
                Open full request
              </button>
            )}
            <button className="icon-button" onClick={onClose} title="Close">×</button>
          </div>
        </header>

        {error && <div className="error-box">{error}</div>}
        {!detail && !error && <div className="empty">Loading…</div>}

        {detail && (
          <>
            <div className="drawer-summary">
              <span className={statusClass(detail.status_code)}>{detail.status_code || "ERR"}</span>
              <span>{detail.provider || "unknown provider"}</span>
              {detail.model && <span className="muted">{detail.model}</span>}
              {detail.harness && <span className="chip">{detail.harness}</span>}
              {detail.streamed && <span className="chip">streamed</span>}
              {detail.incomplete && <span className="chip warn">incomplete</span>}
              {detail.has_tool_calls && <span className="chip">tools</span>}
            </div>

            {detail.parse_error && (
              <div className="warn-box">
                <strong>Capture warning:</strong> {detail.parse_error}
              </div>
            )}

            <nav className="tabs">
              {(["payload", "http", "usage"] as Tab[]).map((t) => (
                <button key={t} className={tab === t ? "active" : ""} onClick={() => setTab(t)}>
                  {t === "payload" ? "Payload" : t === "http" ? "HTTP" : "Usage & cost"}
                </button>
              ))}
            </nav>

            <div className="drawer-body">
              {tab === "payload" && (
                <PayloadView
                  requestBody={detail.request_body}
                  responseBody={detail.response_body}
                  streamed={detail.streamed}
                  rawResponseBody={detail.raw_response_body}
                />
              )}
              {tab === "http" && <HTTPTab detail={detail} />}
              {tab === "usage" && <UsageTab detail={detail} />}
            </div>
          </>
        )}
      </aside>
    </div>
  );
}

function HTTPTab({ detail }: { detail: RequestDetail }) {
  return (
    <>
      <section>
        <h3>HTTP metadata</h3>
        <dl className="kv">
          <dt>Method</dt><dd>{detail.method}</dd>
          <dt>Local path</dt><dd><code>{detail.local_path}</code></dd>
          <dt>Upstream URL</dt><dd><code>{detail.upstream_url}</code> <CopyButton text={detail.upstream_url} /></dd>
          <dt>Status</dt><dd>{detail.status_code}</dd>
          <dt>Started</dt><dd>{formatTime(detail.started_at)}</dd>
          <dt>Completed</dt><dd>{formatTime(detail.completed_at)}</dd>
          <dt>Duration</dt><dd>{formatDuration(detail.started_at, detail.completed_at)}</dd>
          <dt>Provider</dt><dd>{detail.provider || "—"}</dd>
          <dt>Model</dt><dd>{detail.model || "—"}</dd>
          <dt>Harness</dt><dd>{detail.harness || "—"}</dd>
          <dt>Parser</dt><dd>{detail.parser || "—"}</dd>
          <dt>Session</dt><dd>{detail.session_key || "—"}{detail.session_header ? ` (${detail.session_header})` : ""}</dd>
          <dt>Request content type</dt><dd>{detail.request_content_type || "—"}</dd>
          <dt>Response content type</dt><dd>{detail.response_content_type || "—"}</dd>
          <dt>Streamed</dt><dd>{detail.streamed ? "yes" : "no"}</dd>
          <dt>Incomplete</dt><dd>{detail.incomplete ? "yes" : "no"}</dd>
        </dl>
      </section>
      <section>
        <h3>Extracted headers</h3>
        <JSONView value={detail.metadata_json} />
        <p className="muted small">Full headers are not stored. Only configured session and metadata headers are extracted.</p>
      </section>
    </>
  );
}

function UsageTab({ detail }: { detail: RequestDetail }) {
  const nonCached = nonCachedInputTokens(detail.input_tokens, detail.cached_input_tokens);
  return (
    <>
      <section>
        <h3>Tokens</h3>
        <dl className="kv">
          <dt>Input</dt><dd>{formatTokens(detail.input_tokens)}</dd>
          <dt>Cached input</dt><dd>{formatTokens(detail.cached_input_tokens)}</dd>
          <dt>Non-cached input</dt><dd>{formatTokens(nonCached)}</dd>
          <dt>Generated</dt><dd>{formatTokens(detail.output_tokens)}</dd>
          <dt>Reasoning</dt><dd>{formatTokens(detail.reasoning_tokens)}</dd>
          <dt>Total</dt><dd>{formatTokens(detail.total_tokens)}</dd>
          <dt>Cache read</dt><dd>{formatTokens(detail.cache_read_tokens)}</dd>
          <dt>Cache write</dt><dd>{formatTokens(detail.cache_write_tokens)}</dd>
        </dl>
        <p className="muted small">
          Non-cached input is input minus cached input when both are reported. Reasoning tokens are
          included in generated tokens when reported by the provider.
        </p>
      </section>
      <section>
        <h3>Cost</h3>
        <dl className="kv">
          <dt>Final cost</dt><dd>{formatCost(detail.cost_amount)}</dd>
          <dt>Source</dt><dd>{detail.cost_source || "unavailable"}</dd>
          <dt>Provider reported</dt><dd>{formatCost(detail.provider_cost)}</dd>
          <dt>Calculated</dt><dd>{formatCost(detail.calculated_cost)}</dd>
        </dl>
      </section>
      <section>
        <h3>Usage data</h3>
        <JSONView value={detail.usage_json} />
      </section>
      <section>
        <h3>Cost explanation</h3>
        <JSONView value={detail.cost_details_json} />
      </section>
      {detail.pricing_snapshot_json && (
        <section>
          <h3>Pricing snapshot</h3>
          <JSONView value={detail.pricing_snapshot_json} />
        </section>
      )}
    </>
  );
}
