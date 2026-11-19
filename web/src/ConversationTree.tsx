// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useMemo, useState, type ReactNode } from "react";
import type { ConversationNode } from "./types";
import { prettyJSON } from "./format";
import { CollapsibleText } from "./CollapsibleText";
import { CopyButton } from "./CopyButton";
import { MessageText } from "./MessageText";

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
          if (e.target !== e.currentTarget) return;
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

            {node.content && (
              <MessageText
                text={node.content}
                maxLines={20}
                maxChars={4000}
                className="message-content"
                defaultFormatted={node.role === "assistant"}
              />
            )}

            {node.reasoning && (
              <LazyDetails className="reasoning" summary="Reasoning">
                <MessageText text={node.reasoning} maxLines={40} maxChars={8000} defaultFormatted full />
              </LazyDetails>
            )}

            {toolCalls.length > 0 && (
              <div className="tool-calls">
                {toolCalls.map((call, idx) => {
                  const results = call.id ? node.toolResultsByCallId?.[call.id] ?? [] : [];
                  return <ToolCall key={call.id || idx} call={call} index={idx} results={results} />;
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

type LazyDetailsProps = {
  className?: string;
  summary: ReactNode;
  children: ReactNode;
};

// LazyDetails keeps closed content out of the React tree. Conversation pages
// can contain hundreds of reasoning, tool-call and tool-result blocks, and
// rendering hidden content is expensive.
function LazyDetails({ className, summary, children }: LazyDetailsProps) {
  const [open, setOpen] = useState(false);
  return (
    <details
      className={className}
      open={open}
      onToggle={(e) => setOpen(e.currentTarget.open)}
      onClick={(e) => e.stopPropagation()}
    >
      <summary>{summary}</summary>
      {open && children}
    </details>
  );
}

function ToolCall({ call, index, results }: { call: ToolCallView; index: number; results: ConversationNode[] }) {
  const [raw, setRaw] = useState(false);
  const rawJSON = useMemo(() => (raw ? formatToolCallJSON(call, results) : ""), [raw, call, results]);
  return (
    <LazyDetails
      className="tool-call"
      summary={
        <span className="tool-call-head">
          <span className="tool-call-index">#{index + 1}</span>
          <span className="tool-call-name">{call.name || "(unnamed tool)"}</span>
        </span>
      }
    >
      <div className="tool-call-toolbar">
        <button
          type="button"
          className="link-button"
          onClick={(e) => {
            e.stopPropagation();
            setRaw((v) => !v);
          }}
        >
          {raw ? "show formatted" : "show raw"}
        </button>
      </div>
      {raw ? (
        <div className="tool-call-raw">
          <CopyButton text={rawJSON} />
          <pre>{rawJSON}</pre>
        </div>
      ) : (
        <>
          {call.arguments !== undefined && <ToolArguments value={call.arguments} full />}
          {results.length > 0 && <ToolResults results={results} />}
        </>
      )}
    </LazyDetails>
  );
}

function ToolResults({ results }: { results: ConversationNode[] }) {
  return (
    <div className="tool-result-group">
      {results.map((result) => (
        <div className="tool-result" key={result.id}>
          <div className="tool-result-label">Result</div>
          {result.content ? (
            <pre className="tool-result-content">{result.content}</pre>
          ) : (
            <div className="muted small">(empty result)</div>
          )}
        </div>
      ))}
    </div>
  );
}

type ToolCallView = { id?: string; type?: string; name?: string; arguments?: unknown };

function parseToolArguments(value: unknown): unknown {
  if (typeof value !== "string") return value;
  try {
    return JSON.parse(value);
  } catch {
    return value;
  }
}

function formatToolArguments(value: unknown): string | undefined {
  if (value === undefined) return undefined;
  const parsed = parseToolArguments(value);
  if (typeof parsed === "string") return parsed;
  return prettyJSON(parsed);
}

function formatToolCallJSON(call: ToolCallView, results: ConversationNode[]): string {
  const payload: Record<string, unknown> = {};
  if (call.id !== undefined) payload.id = call.id;
  if (call.type !== undefined) payload.type = call.type;
  if (call.name !== undefined) payload.name = call.name;
  if (call.arguments !== undefined) payload.arguments = parseToolArguments(call.arguments);
  if (results.length > 0) {
    payload.results = results.map((result) => ({
      tool_call_id: result.tool_call_id,
      name: result.name,
      content: parseMaybeJSON(result.content ?? "")
    }));
  }
  return JSON.stringify(payload, null, 2);
}

function parseMaybeJSON(value: string): unknown {
  const trimmed = value.trim();
  if (!trimmed || (!trimmed.startsWith("{") && !trimmed.startsWith("["))) {
    return value;
  }
  try {
    return JSON.parse(trimmed);
  } catch {
    return value;
  }
}

function ToolArguments({ value, full = false }: { value: unknown; full?: boolean }) {
  if (value === undefined || value === null || value === "") return null;
  const parsed = parseToolArguments(value);
  if (typeof parsed === "string") {
    if (!parsed) return null;
    return full ? <pre>{parsed}</pre> : <CollapsibleText text={parsed} maxLines={20} maxChars={4000} />;
  }
  if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
    const entries = Object.entries(parsed as Record<string, unknown>);
    if (entries.some(([, field]) => typeof field === "string" && field.includes("\n"))) {
      return (
        <div className="tool-args">
          {entries.map(([key, field]) => (
            <div className="tool-arg" key={key}>
              <span className="tool-arg-key">{key}</span>
              {typeof field === "string" ? (
                full ? (
                  <pre>{field}</pre>
                ) : (
                  <CollapsibleText text={field} maxLines={20} maxChars={4000} />
                )
              ) : (
                <pre>{JSON.stringify(field) ?? String(field)}</pre>
              )}
            </div>
          ))}
        </div>
      );
    }
  }
  const text = formatToolArguments(value) ?? "";
  return full ? <pre>{text}</pre> : <CollapsibleText text={text} maxLines={20} maxChars={4000} />;
}

function parseToolCalls(value: unknown): ToolCallView[] {
  if (!value) return [];
  const list = Array.isArray(value) ? value : [];
  return list.map((item) => {
    if (item && typeof item === "object") {
      const obj = item as Record<string, unknown>;
      const fn = (obj.function ?? {}) as Record<string, unknown>;
      return {
        id: typeof obj.id === "string" ? obj.id : undefined,
        type: typeof obj.type === "string" ? obj.type : undefined,
        name: typeof fn.name === "string" ? fn.name : undefined,
        arguments: fn.arguments
      };
    }
    return { arguments: item };
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
