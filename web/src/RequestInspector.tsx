// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import type { RequestDetail } from "./types";
import { ConversationTree } from "./ConversationTree";
import { PayloadView } from "./PayloadView";
import { JSONView } from "./JSONView";
import { formatCost, formatDuration, formatTime, formatTokens, statusClass } from "./format";
import { TokenBreakdown } from "./TokenBreakdown";
import { CopyButton } from "./CopyButton";

type Props = {
  requestId: number;
  refreshKey: number;
  onBack: () => void;
  onOpenSession: (id: number) => void;
  prevRequestId?: number;
  nextRequestId?: number;
  onNavigateRequest: (id: number) => void;
};

type PaneMode = "conversation" | "payload";

export function RequestInspector({
  requestId,
  refreshKey,
  onBack,
  onOpenSession,
  prevRequestId,
  nextRequestId,
  onNavigateRequest
}: Props) {
  const [detail, setDetail] = useState<RequestDetail | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [selectedNodeId, setSelectedNodeId] = useState<string | undefined>();
  const [paneMode, setPaneMode] = useState<PaneMode>("conversation");

  const lastRequestId = useRef(requestId);

  useEffect(() => {
    if (lastRequestId.current !== requestId) {
      lastRequestId.current = requestId;
      setDetail(null);
      setSelectedNodeId(undefined);
      setPaneMode("conversation");
    }
  }, [requestId]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");
    api
      .request(requestId)
      .then((d) => {
        if (!cancelled) {
          setDetail(d);
          setPaneMode((prev) => {
            if (prev === "payload") return "payload";
            return (d.conversation?.roots?.length ?? 0) > 0 ? "conversation" : "payload";
          });
          setLoading(false);
        }
      })
      .catch((e) => {
        if (!cancelled) {
          setError(String(e));
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [requestId, refreshKey]);

  if (loading && !detail) return <main className="page"><div className="empty">Loading request…</div></main>;
  if (error && !detail) return <main className="page"><div className="error-box">{error}</div></main>;
  if (!detail) return null;

  const roots = detail.conversation?.roots ?? [];
  const hasConversation = roots.length > 0;
  const hasTokens =
    detail.input_tokens !== undefined ||
    detail.output_tokens !== undefined ||
    detail.cached_input_tokens !== undefined ||
    detail.reasoning_tokens !== undefined;
  const hasUsageDetails =
    Boolean(detail.usage_json) ||
    Boolean(detail.cost_details_json) ||
    Boolean(detail.pricing_snapshot_json);

  return (
    <main className="page inspector-page">
      {error && <div className="error-box">{error}</div>}
      <div className="inspector-head">
        <button className="back-button" onClick={onBack}>← Requests</button>
        <div className="inspector-nav">
          <button
            className="secondary"
            disabled={prevRequestId === undefined}
            onClick={() => prevRequestId !== undefined && onNavigateRequest(prevRequestId)}
            title="Previous request in the current list"
          >
            ← Prev
          </button>
          <button
            className="secondary"
            disabled={nextRequestId === undefined}
            onClick={() => nextRequestId !== undefined && onNavigateRequest(nextRequestId)}
            title="Next request in the current list"
          >
            Next →
          </button>
        </div>
        <div className="inspector-title">
          <h1>
            Request #{detail.id}
            <CopyButton text={String(detail.id)} className="title-copy" />
          </h1>
          <p className="muted">
            <span className={statusClass(detail.status_code)}>{detail.status_code || "ERR"}</span>{" "}
            {detail.method} {[detail.provider, detail.endpoint].filter(Boolean).join(" ")} · {formatTime(detail.started_at)}
            {detail.model && <> · {detail.model}</>}
            {detail.harness && <span className="chip">{detail.harness}</span>}
            {detail.streamed && <span className="chip">streamed</span>}
            {detail.incomplete && <span className="chip warn">incomplete</span>}
            {detail.has_tool_calls && <span className="chip">tools</span>}
          </p>
        </div>
        <div className="summary-stats">
          <div className="stat token-stat">
            <div className="stat-label">Tokens</div>
            <div className="stat-value">{formatTokens(detail.total_tokens)}</div>
            {hasTokens && (
              <TokenBreakdown
                input={detail.input_tokens}
                cached={detail.cached_input_tokens}
                generated={detail.output_tokens}
                reasoning={detail.reasoning_tokens}
                compact
              />
            )}
          </div>
          <Stat label="Cost" value={formatCost(detail.cost_amount)} />
          <Stat label="Session" value={detail.session_key || "unassigned"} />
        </div>
      </div>

      <div className="inspector-note">
        {paneMode === "conversation"
          ? "Conversation reconstructed from this request body and final response. Streaming responses use the reconstructed final JSON."
          : "Payload from this request and final response. Chat completions are pretty-printed; other formats are shown raw."}
      </div>

      <div className="inspector-grid">
        <section className="pane">
          <div className="pane-head">
            <h2>{paneMode === "conversation" ? "Conversation" : "Payload"}</h2>
            <div className="segmented">
              <button
                className={paneMode === "conversation" ? "active" : ""}
                onClick={() => setPaneMode("conversation")}
                disabled={!hasConversation}
                title={hasConversation ? "Show conversation" : "No parsed conversation for this request"}
              >
                Conversation
              </button>
              <button
                className={paneMode === "payload" ? "active" : ""}
                onClick={() => setPaneMode("payload")}
              >
                Payload
              </button>
            </div>
          </div>
          {paneMode === "conversation" ? (
            <ConversationTree
              roots={roots}
              selectedNodeId={selectedNodeId}
              selectedRequestIds={[]}
              onSelect={(node) => setSelectedNodeId(node.id)}
            />
          ) : (
            <div className="drawer-body">
              <PayloadView
                requestBody={detail.request_body}
                responseBody={detail.response_body}
                streamed={detail.streamed}
                rawResponseBody={detail.raw_response_body}
              />
            </div>
          )}
        </section>

        <section className="pane">
          <div className="pane-head">
            <h2>Request details</h2>
            <span className="muted small">metadata</span>
          </div>
          <div className="drawer-body">
            <section>
              <h3>Request</h3>
              <dl className="kv">
                <dt>Request ID</dt><dd>#{detail.id} <CopyButton text={String(detail.id)} /></dd>
                <dt>Endpoint</dt><dd><code>{detail.endpoint}</code></dd>
                <dt>Status</dt><dd><span className={statusClass(detail.status_code)}>{detail.status_code || "ERR"}</span></dd>
                <dt>Provider</dt><dd>{detail.provider || "—"}</dd>
                <dt>Model</dt><dd>{detail.model || "—"}</dd>
                <dt>Harness</dt><dd>{detail.harness || "—"}</dd>
                <dt>Session</dt><dd>{detail.session_key || "unassigned"}</dd>
                <dt>Started</dt><dd>{formatTime(detail.started_at)}</dd>
                <dt>Completed</dt><dd>{formatTime(detail.completed_at)}</dd>
                <dt>Duration</dt><dd>{formatDuration(detail.started_at, detail.completed_at)}</dd>
                <dt>Parser</dt><dd>{detail.parser || "—"}</dd>
              </dl>
            </section>

            <section>
              <h3>Tokens & cost</h3>
              <dl className="kv">
                <dt>Input</dt><dd>{formatTokens(detail.input_tokens)}</dd>
                <dt>Cached input</dt><dd>{formatTokens(detail.cached_input_tokens)}</dd>
                <dt>Non-cached input</dt>
                <dd>{formatTokens(detail.input_tokens !== undefined && detail.cached_input_tokens !== undefined ? Math.max(0, detail.input_tokens - detail.cached_input_tokens) : undefined)}</dd>
                <dt>Generated</dt><dd>{formatTokens(detail.output_tokens)}</dd>
                <dt>Reasoning</dt><dd>{formatTokens(detail.reasoning_tokens)}</dd>
                <dt>Total</dt><dd>{formatTokens(detail.total_tokens)}</dd>
                <dt>Cost</dt><dd>{formatCost(detail.cost_amount)}</dd>
                <dt>Cost source</dt><dd>{detail.cost_source || "unavailable"}</dd>
              </dl>
            </section>

            {hasUsageDetails && (
              <section>
                <details className="detail-disclosure">
                  <summary>Usage & cost details</summary>
                  {detail.usage_json && (
                    <div className="detail-section">
                      <h3>Usage data</h3>
                      <JSONView value={detail.usage_json} />
                    </div>
                  )}
                  {detail.cost_details_json && (
                    <div className="detail-section">
                      <h3>Cost explanation</h3>
                      <JSONView value={detail.cost_details_json} />
                    </div>
                  )}
                  {detail.pricing_snapshot_json && (
                    <div className="detail-section">
                      <h3>Pricing snapshot</h3>
                      <JSONView value={detail.pricing_snapshot_json} />
                    </div>
                  )}
                </details>
              </section>
            )}

            {detail.session_id !== undefined && (
              <div className="button-row">
                <button className="secondary" onClick={() => onOpenSession(detail.session_id!)}>Open session</button>
              </div>
            )}
          </div>
        </section>
      </div>
    </main>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="stat">
      <div className="stat-label">{label}</div>
      <div className="stat-value">{value}</div>
    </div>
  );
}
