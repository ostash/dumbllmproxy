// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import type { RequestSummary } from "./types";
import { formatCost, formatDuration, formatRelativeTime, formatTime, formatTokens, statusClass } from "./format";
import { TokenBreakdown } from "./TokenBreakdown";

type Props = {
  requests: RequestSummary[];
  selectedRequestIds: number[];
  onSelect: (request: RequestSummary) => void;
};

export function RequestTimeline({ requests, selectedRequestIds, onSelect }: Props) {
  if (requests.length === 0) {
    return <div className="empty">No HTTP requests.</div>;
  }
  return (
    <div className="timeline">
      {requests.map((req) => {
        const selected = selectedRequestIds.includes(req.id);
        return (
          <button
            key={req.id}
            id={`req-${req.id}`}
            className={`timeline-item ${selected ? "selected" : ""}`}
            onClick={() => onSelect(req)}
          >
            <div className="timeline-top">
              <span className={statusClass(req.status_code)}>{req.status_code || "ERR"}</span>
              <span className="timeline-id">HTTP #{req.id}</span>
              <span className="timeline-time" title={formatTime(req.started_at)}>{formatRelativeTime(req.started_at)}</span>
            </div>
            <div className="timeline-meta">
              <span>{req.provider || "unknown provider"}</span>
              {req.model && <span className="muted"> · {req.model}</span>}
              {req.harness && <span className="chip">{req.harness}</span>}
              {req.streamed && <span className="chip">streamed</span>}
              {req.incomplete && <span className="chip warn">incomplete</span>}
            </div>
            <div className="timeline-foot">
              <span>{req.method}</span>
              <span className="muted">{req.local_path}</span>
              <span className="muted">duration {formatDuration(req.started_at, req.completed_at)}</span>
              <span className="muted">tokens {formatTokens(req.total_tokens)}</span>
              <span className="muted">cost {formatCost(req.cost_amount)}</span>
            </div>
            {(req.input_tokens !== undefined ||
              req.output_tokens !== undefined ||
              req.cached_input_tokens !== undefined ||
              req.reasoning_tokens !== undefined) && (
              <div className="timeline-tokens">
                <TokenBreakdown
                  input={req.input_tokens}
                  cached={req.cached_input_tokens}
                  generated={req.output_tokens}
                  reasoning={req.reasoning_tokens}
                  compact
                />
              </div>
            )}
            {req.parse_error && <div className="parse-error small">{req.parse_error}</div>}
          </button>
        );
      })}
    </div>
  );
}
