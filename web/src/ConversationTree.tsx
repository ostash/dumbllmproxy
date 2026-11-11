// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useMemo, useState } from "react";
import type { ConversationNode } from "./types";
import { prettyJSON } from "./format";
import { CollapsibleText } from "./CollapsibleText";

type Props = {
  roots: ConversationNode[];
  selectedNodeId?: string;
  selectedRequestIds: number[];
  onSelect: (node: ConversationNode) => void;
  onOpenRequest?: (requestId: number) => void;
};

type DisplayNode = Omit<ConversationNode, "children"> & {
  children: DisplayNode[];
  toolResultsByCallId?: Record<string, ConversationNode[]>;
};

export function ConversationTree({ roots, selectedNodeId, selectedRequestIds, onSelect, onOpenRequest }: Props) {
  const displayRoots = useMemo(() => buildDisplayTree(roots), [roots]);
  const toolCallsById = useMemo(() => collectToolCalls(displayRoots), [displayRoots]);
  if (roots.length === 0) {
    return <div className="empty">No parsed conversation messages. Open the payload details for raw content.</div>;
  }
  return (
    <div className="tree">
      {displayRoots.map((node) => (
        <NodeRow
          key={node.id}
          node={node}
          depth={0}
          toolCallsById={toolCallsById}
          selectedNodeId={selectedNodeId}
          selectedRequestIds={selectedRequestIds}
          onSelect={onSelect}
          onOpenRequest={onOpenRequest}
        />
      ))}
    </div>
  );
}

type ToolCallInfo = { id: string; name?: string; index: number };

type RowProps = {
  node: DisplayNode;
  depth: number;
  toolCallsById: Map<string, ToolCallInfo>;
  selectedNodeId?: string;
  selectedRequestIds: number[];
  onSelect: (node: ConversationNode) => void;
  onOpenRequest?: (requestId: number) => void;
};

function NodeRow({ node, depth, toolCallsById, selectedNodeId, selectedRequestIds, onSelect, onOpenRequest }: RowProps) {
  const [bodyExpanded, setBodyExpanded] = useState(node.role !== "tool");
  const [childrenExpanded, setChildrenExpanded] = useState(true);
  const highlighted =
    !selectedNodeId &&
    selectedRequestIds.length > 0 &&
    selectedRequestIds.includes(node.source_request_id);
  const selected = selectedNodeId === node.id;
  const children = node.children ?? [];
  const toolCalls = parseToolCalls(node.tool_calls);
  const hasBody = Boolean(node.content || node.reasoning || toolCalls.length || node.name || node.tool_call_id);
  const isBranch = children.length > 1;
  const childDepth = depth + (isBranch ? 1 : 0);
  const toolCall = node.tool_call_id ? toolCallsById.get(node.tool_call_id) : undefined;

  return (
    <div className={`tree-branch ${isBranch ? "has-branch" : ""}`.trim()}>
      <div
        className={`tree-node ${node.role === "tool" ? "tool-result-node" : ""} ${highlighted ? "highlighted" : ""} ${selected ? "selected" : ""}`}
        style={{ marginLeft: depth * 18 }}
        tabIndex={0}
        role="button"
        onClick={() => onSelect(node)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onSelect(node);
          }
        }}
      >
        <div className="tree-node-head">
          {hasBody ? (
            <button
              className="tree-toggle"
              onClick={(e) => {
                e.stopPropagation();
                setBodyExpanded((v) => !v);
              }}
              title={bodyExpanded ? "Collapse message" : "Expand message"}
            >
              {bodyExpanded ? "▾" : "▸"}
            </button>
          ) : (
            <span className="tree-toggle-placeholder" />
          )}
          <span className={`role role-${node.role}`}>{node.role}</span>
          {onOpenRequest ? (
            <button
              className="request-link"
              onClick={(e) => {
                e.stopPropagation();
                onOpenRequest(node.source_request_id);
              }}
              title={`Open source request #${node.source_request_id}`}
            >
              HTTP #{node.source_request_id}
            </button>
          ) : (
            <span className="muted small">HTTP #{node.source_request_id}</span>
          )}
          {node.role === "tool" && (
            <span className="chip tool-result-chip" title={node.tool_call_id}>
              {toolCall ? `↳ ${toolCall.name || "tool"} #${toolCall.index} result` : "↳ tool result"}
            </span>
          )}
          {isBranch && (
            <button
              className="tree-branch-toggle"
              onClick={(e) => {
                e.stopPropagation();
                setChildrenExpanded((v) => !v);
              }}
              title={childrenExpanded ? "Collapse replies" : "Expand replies"}
            >
              {childrenExpanded ? `hide ${children.length} replies` : `show ${children.length} replies`}
            </button>
          )}
        </div>

        {bodyExpanded && (
          <>
            {node.name && <div className="muted small">name: {node.name}</div>}
            {node.tool_call_id && <div className="muted small">tool_call_id: {node.tool_call_id}</div>}

            {node.content ? (
              <CollapsibleText text={node.content} limit={1200} className="message-content" />
            ) : (
              <div className="muted small">(no text content)</div>
            )}

            {node.reasoning && (
              <details className="reasoning" onClick={(e) => e.stopPropagation()}>
                <summary>Reasoning</summary>
                <CollapsibleText text={node.reasoning} limit={4000} />
              </details>
            )}

            {toolCalls.length > 0 && (
              <div className="tool-calls">
                {toolCalls.map((call, idx) => {
                  const results = call.id ? node.toolResultsByCallId?.[call.id] ?? [] : [];
                  return (
                    <div className="tool-call" key={call.id || idx}>
                      <div className="tool-call-head">
                        <span className="tool-call-index">#{idx + 1}</span>
                        <span className="tool-call-name">{call.name || "(unnamed tool)"}</span>
                      </div>
                      {call.arguments && <CollapsibleText text={call.arguments} limit={1200} />}
                      {results.length > 0 && <ToolResults results={results} />}
                    </div>
                  );
                })}
              </div>
            )}
          </>
        )}
      </div>

      {childrenExpanded &&
        children.map((child) => (
          <NodeRow
            key={child.id}
            node={child}
            depth={childDepth}
            toolCallsById={toolCallsById}
            selectedNodeId={selectedNodeId}
            selectedRequestIds={selectedRequestIds}
            onSelect={onSelect}
            onOpenRequest={onOpenRequest}
          />
        ))}
    </div>
  );
}

function ToolResults({ results }: { results: ConversationNode[] }) {
  return (
    <div className="tool-result-group">
      {results.map((result) => (
        <details className="tool-result" key={result.id} onClick={(e) => e.stopPropagation()}>
          <summary>Result</summary>
          {result.content ? (
            <CollapsibleText text={result.content} limit={4000} />
          ) : (
            <div className="muted small">(empty result)</div>
          )}
        </details>
      ))}
    </div>
  );
}

type ToolCallView = { id?: string; name?: string; arguments?: string };

function parseToolCalls(value: unknown): ToolCallView[] {
  if (!value) return [];
  const list = Array.isArray(value) ? value : [];
  return list.map((item) => {
    if (item && typeof item === "object") {
      const obj = item as Record<string, unknown>;
      const fn = (obj.function ?? {}) as Record<string, unknown>;
      return {
        id: typeof obj.id === "string" ? obj.id : undefined,
        name: typeof fn.name === "string" ? fn.name : undefined,
        arguments:
          typeof fn.arguments === "string"
            ? fn.arguments
            : fn.arguments !== undefined
              ? prettyJSON(fn.arguments)
              : undefined
      };
    }
    return { arguments: prettyJSON(item) };
  });
}

function collectToolCalls(roots: DisplayNode[]): Map<string, ToolCallInfo> {
  const out = new Map<string, ToolCallInfo>();
  const visit = (node: DisplayNode) => {
    parseToolCalls(node.tool_calls).forEach((call, index) => {
      if (call.id && !out.has(call.id)) {
        out.set(call.id, { id: call.id, name: call.name, index: index + 1 });
      }
    });
    node.children?.forEach(visit);
  };
  roots.forEach(visit);
  return out;
}

function buildDisplayTree(roots: ConversationNode[]): DisplayNode[] {
  return roots.map((node) => {
    const display: DisplayNode = {
      ...node,
      children: buildDisplayTree(node.children ?? [])
    };
    if (display.role !== "assistant") {
      return display;
    }
    const callIds = new Set(
      parseToolCalls(display.tool_calls)
        .map((call) => call.id)
        .filter((id): id is string => Boolean(id))
    );
    if (callIds.size === 0 || display.children.length === 0) {
      return display;
    }

    const results: Record<string, ConversationNode[]> = {};
    let rest = display.children;
    while (rest.length > 0) {
      const first = rest[0];
      if (first.role !== "tool" || !first.tool_call_id || !callIds.has(first.tool_call_id)) {
        break;
      }
      (results[first.tool_call_id] ??= []).push(first);
      rest = first.children ?? [];
    }
    if (Object.keys(results).length > 0) {
      display.toolResultsByCallId = results;
      display.children = rest;
    }
    return display;
  });
}
