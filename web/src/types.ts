// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

export type SessionSummary = {
  id: number;
  session_key: string;
  first_seen: string;
  last_activity: string;
  request_count: number;
  providers: string[] | null;
  models: string[] | null;
  input_tokens: number;
  output_tokens: number;
  cached_input_tokens?: number;
  reasoning_tokens?: number;
  total_tokens: number;
  cost_amount?: number;
  usage_reported_requests: number;
  cost_reported_requests: number;
  failed_requests: number;
  incomplete_requests: number;
};

export type Facets = {
  providers: string[];
  models: string[];
  harnesses: string[];
};

export type RequestSummary = {
  id: number;
  session_id?: number;
  session_key: string;
  started_at: string;
  completed_at?: string;
  method: string;
  endpoint: string;
  provider: string;
  model: string;
  harness?: string;
  status_code: number;
  streamed: boolean;
  incomplete: boolean;
  has_tool_calls: boolean;
  parser: string;
  parse_error?: string;
  input_tokens?: number;
  output_tokens?: number;
  cached_input_tokens?: number;
  reasoning_tokens?: number;
  total_tokens?: number;
  cost_amount?: number;
  cost_source?: string;
};

export type ConversationNode = {
  id: string;
  parent_id?: string;
  role: string;
  content?: string;
  reasoning?: string;
  tool_calls?: unknown;
  tool_call_id?: string;
  name?: string;
  source_request_id: number;
  request_ids: number[];
  children?: ConversationNode[];
};

export type SessionDetail = {
  summary: SessionSummary;
  requests: RequestSummary[];
  conversation: { roots: ConversationNode[] };
};

export type RequestDetail = RequestSummary & {
  request_content_type?: string;
  response_content_type?: string;
  metadata_json?: string;
  request_body: string;
  response_body: string;
  raw_response_body?: string;
  usage_json?: string;
  conversation?: { roots: ConversationNode[] };
  cache_read_tokens?: number;
  cache_write_tokens?: number;
  provider_cost?: number;
  calculated_cost?: number;
  cost_details_json?: string;
  pricing_snapshot_json?: string;
};
