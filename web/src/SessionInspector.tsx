// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import type { ConversationNode, RequestSummary, SessionDetail } from "./types";
import { ConversationTree } from "./ConversationTree";
import { RequestTimeline } from "./RequestTimeline";
import { RequestDrawer } from "./RequestDrawer";
import { formatCost, formatTime, formatTokens } from "./format";
import { TokenBreakdown } from "./TokenBreakdown";
import { PartialChip, hasNoData } from "./Completeness";
import { CopyButton } from "./CopyButton";

type Props = {
  sessionId: number;
  refreshKey: number;
  onBack: () => void;
  onOpenRequest: (id: number) => void;
  initialNodeId?: string;
};

export function SessionInspector({ sessionId, refreshKey, onBack, onOpenRequest, initialNodeId }: Props) {
  const [detail, setDetail] = useState<SessionDetail | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [selectedNodeId, setSelectedNodeId] = useState<string | undefined>();
  const [selectedRequestIds, setSelectedRequestIds] = useState<number[]>([]);
  const [drawerRequestId, setDrawerRequestId] = useState<number | null>(null);
  const initialNodeApplied = useRef(false);

  useEffect(() => {
    setDetail(null);
    setSelectedNodeId(undefined);
    setSelectedRequestIds([]);
    setDrawerRequestId(null);
    initialNodeApplied.current = false;
  }, [sessionId]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");
    api
      .session(sessionId)
      .then((d) => {
        if (!cancelled) {
          setDetail(d);
          if (!initialNodeApplied.current && initialNodeId) {
            initialNodeApplied.current = true;
            const node = findNode(d.conversation.roots, initialNodeId);
            if (node) {
              setSelectedNodeId(node.id);
              setSelectedRequestIds(node.request_ids);
              setTimeout(() => {
                document.getElementById(`req-${node.source_request_id}`)?.scrollIntoView({ block: "nearest" });
              }, 0);
            }
          }
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
  }, [sessionId, refreshKey]);

  function selectNode(node: ConversationNode) {
    setSelectedNodeId(node.id);
    setSelectedRequestIds(node.request_ids);
    window.history.replaceState(null, "", `#/session/${sessionId}?node=${encodeURIComponent(node.id)}`);
    setTimeout(() => {
      document.getElementById(`req-${node.source_request_id}`)?.scrollIntoView({ block: "nearest" });
    }, 0);
  }

  function openNodeRequest(requestId: number) {
    setDrawerRequestId(requestId);
    setTimeout(() => {
      document.getElementById(`req-${requestId}`)?.scrollIntoView({ block: "nearest" });
    }, 0);
  }

  function selectRequest(request: RequestSummary) {
    setSelectedNodeId(undefined);
    setSelectedRequestIds([request.id]);
    setDrawerRequestId(request.id);
    window.history.replaceState(null, "", `#/session/${sessionId}`);
  }

  if (loading && !detail) return <main className="page"><div className="empty">Loading session…</div></main>;
  if (error && !detail) return <main className="page"><div className="error-box">{error}</div></main>;
  if (!detail) return null;

  const summary = detail.summary;
  return (
    <main className="page inspector-page">
      {error && <div className="error-box">{error}</div>}
      <div className="inspector-head">
        <button className="back-button" onClick={onBack}>← Sessions</button>
        <div className="inspector-title">
          <h1>
            {summary.session_key}
            <CopyButton text={summary.session_key} className="title-copy" />
          </h1>
          <p className="muted">
            {summary.request_count} HTTP exchange{summary.request_count === 1 ? "" : "s"} · started {formatTime(summary.first_seen)} · last {formatTime(summary.last_activity)}
          </p>
        </div>
        <div className="summary-stats">
          <div className="stat token-stat">
            <div className="stat-label">Tokens</div>
            <div className="stat-value">
              {hasNoData(summary.usage_reported_requests) ? "—" : formatTokens(summary.total_tokens)}
              <PartialChip reported={summary.usage_reported_requests} total={summary.request_count} label="usage" />
            </div>
            {!hasNoData(summary.usage_reported_requests) &&
              (summary.total_tokens > 0 || summary.cached_input_tokens !== undefined || summary.reasoning_tokens !== undefined) && (
                <TokenBreakdown
                  input={summary.input_tokens}
                  cached={summary.cached_input_tokens}
                  generated={summary.output_tokens}
                  reasoning={summary.reasoning_tokens}
                  compact
                />
              )}
          </div>
          <div className="stat">
            <div className="stat-label">Cost</div>
            <div className="stat-value">
              {hasNoData(summary.cost_reported_requests) ? "—" : formatCost(summary.cost_amount)}
              <PartialChip reported={summary.cost_reported_requests} total={summary.request_count} label="cost" />
            </div>
          </div>
          <Stat label="Providers" value={(summary.providers ?? []).join(", ") || "—"} />
          <Stat label="Models" value={(summary.models ?? []).join(", ") || "—"} />
        </div>
      </div>

      <div className="inspector-note">
        The conversation is reconstructed from {detail.requests.length} HTTP exchange{detail.requests.length === 1 ? "" : "s"}.
        Selecting a message highlights the requests where it appears. Use the HTTP link on a message to open its source request. Selecting a request highlights the messages it introduced.
      </div>

      <div className="inspector-grid">
        <section className="pane">
          <div className="pane-head">
            <h2>Conversation</h2>
            <span className="muted small">tree</span>
          </div>
          <ConversationTree
            roots={detail.conversation.roots}
            selectedNodeId={selectedNodeId}
            selectedRequestIds={selectedRequestIds}
            onSelect={selectNode}
            onOpenRequest={openNodeRequest}
          />
        </section>

        <section className="pane">
          <div className="pane-head">
            <h2>HTTP timeline</h2>
            <span className="muted small">chronological</span>
          </div>
          <RequestTimeline
            requests={detail.requests}
            selectedRequestIds={selectedRequestIds}
            onSelect={selectRequest}
          />
        </section>
      </div>

      {drawerRequestId !== null && (
        <RequestDrawer
          requestId={drawerRequestId}
          onClose={() => setDrawerRequestId(null)}
          onOpenRequest={onOpenRequest}
        />
      )}
    </main>
  );
}

function findNode(roots: ConversationNode[], id: string): ConversationNode | undefined {
  for (const node of roots) {
    if (node.id === id) return node;
    const found = findNode(node.children ?? [], id);
    if (found) return found;
  }
  return undefined;
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="stat">
      <div className="stat-label">{label}</div>
      <div className="stat-value">{value}</div>
    </div>
  );
}
