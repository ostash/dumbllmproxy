// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package store

var schema = []string{
	`CREATE TABLE IF NOT EXISTS sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_key TEXT NOT NULL UNIQUE
	)`,
	`CREATE TABLE IF NOT EXISTS requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id INTEGER REFERENCES sessions(id) ON DELETE SET NULL,
		session_key TEXT,
		session_header TEXT,
		started_at TEXT NOT NULL,
		completed_at TEXT,
		method TEXT NOT NULL,
		endpoint TEXT NOT NULL,
		provider TEXT,
		model TEXT,
		status_code INTEGER NOT NULL DEFAULT 0,
		request_content_type TEXT,
		response_content_type TEXT,
		metadata_json TEXT,
		request_body BLOB,
		response_body BLOB,
		raw_response_body BLOB,
		streamed INTEGER NOT NULL DEFAULT 0,
		incomplete INTEGER NOT NULL DEFAULT 0,
		has_tool_calls INTEGER NOT NULL DEFAULT 0,
		parser TEXT,
		parse_error TEXT,
		pricing TEXT,
		usage_json TEXT,
		input_tokens INTEGER,
		output_tokens INTEGER,
		cached_input_tokens INTEGER,
		cache_read_tokens INTEGER,
		cache_write_tokens INTEGER,
		reasoning_tokens INTEGER,
		total_tokens INTEGER,
		provider_cost REAL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_requests_session_started
		ON requests(session_id, started_at)`,
	`CREATE INDEX IF NOT EXISTS idx_requests_started
		ON requests(started_at)`,
}
