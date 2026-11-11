// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"dumbllmproxy/internal/usage"

	_ "modernc.org/sqlite"
)

type Store struct {
	db           *sql.DB
	costResolver CostResolver
}

// CostFacts are the immutable capture facts used to resolve a request's cost
// at read time. They are read from the database, never from stored cost values.
type CostFacts struct {
	Pricing      string
	Model        string
	CapturedAt   time.Time
	Usage        usage.Usage
	ProviderCost *float64
}

// Cost is a resolved cost for display. Amount is nil when cost is unavailable.
type Cost struct {
	Amount       *float64
	Source       string
	DetailsJSON  string
	SnapshotJSON string
}

// CostResolver resolves a request's cost from its capture facts. It is called
// on every read so that calculated costs always reflect the active pricing
// configuration.
type CostResolver func(CostFacts) Cost

// SetCostResolver installs the read-time cost resolver. Without one, only
// provider-reported costs are unavailable too, because nothing is resolved.
func (s *Store) SetCostResolver(r CostResolver) {
	s.costResolver = r
}

func (s *Store) resolveCost(f CostFacts) Cost {
	if s.costResolver == nil {
		return Cost{}
	}
	return s.costResolver(f)
}

type RequestRecord struct {
	SessionKey    string
	SessionHeader string
	StartedAt     time.Time
	CompletedAt   *time.Time

	Method      string
	LocalPath   string
	UpstreamURL string
	Provider    string
	Model       string
	StatusCode  int

	RequestContentType  string
	ResponseContentType string
	MetadataJSON        string

	RequestBody     []byte
	ResponseBody    []byte
	RawResponseBody []byte

	Streamed     bool
	Incomplete   bool
	HasToolCalls bool
	// Conversation marks a model generation. Auxiliary traffic (model
	// listings, presets, health checks) is captured but never joins a session.
	Conversation bool
	Parser       string
	ParseError   string

	UsageJSON string

	InputTokens       *int64
	OutputTokens      *int64
	CachedInputTokens *int64
	CacheReadTokens   *int64
	CacheWriteTokens  *int64
	ReasoningTokens   *int64
	TotalTokens       *int64

	// Pricing is the configured pricing name. It is a capture fact: it records
	// which calculator was selected, not a calculated amount.
	Pricing string

	// ProviderCost is a provider-reported cost (for example OpenRouter's
	// usage.cost). It is a captured fact and stays persisted.
	ProviderCost *float64
}

type SessionSummary struct {
	ID                int64    `json:"id"`
	SessionKey        string   `json:"session_key"`
	FirstSeen         string   `json:"first_seen"`
	LastActivity      string   `json:"last_activity"`
	RequestCount      int64    `json:"request_count"`
	Providers         []string `json:"providers"`
	Models            []string `json:"models"`
	InputTokens       int64    `json:"input_tokens"`
	OutputTokens      int64    `json:"output_tokens"`
	CachedInputTokens *int64   `json:"cached_input_tokens,omitempty"`
	ReasoningTokens   *int64   `json:"reasoning_tokens,omitempty"`
	TotalTokens       int64    `json:"total_tokens"`
	CostAmount        *float64 `json:"cost_amount,omitempty"`
	// UsageReportedRequests and CostReportedRequests count the requests in the
	// session that actually reported usage or cost. They let the UI distinguish
	// "no data" and "partial data" from a genuine zero.
	UsageReportedRequests int64 `json:"usage_reported_requests"`
	CostReportedRequests  int64 `json:"cost_reported_requests"`
	FailedRequests        int64 `json:"failed_requests"`
	IncompleteRequests    int64 `json:"incomplete_requests"`
}

type RequestSummary struct {
	ID                int64    `json:"id"`
	SessionID         *int64   `json:"session_id,omitempty"`
	SessionKey        string   `json:"session_key"`
	SessionHeader     string   `json:"session_header,omitempty"`
	StartedAt         string   `json:"started_at"`
	CompletedAt       string   `json:"completed_at,omitempty"`
	Method            string   `json:"method"`
	LocalPath         string   `json:"local_path"`
	UpstreamURL       string   `json:"upstream_url"`
	Provider          string   `json:"provider"`
	Model             string   `json:"model"`
	Pricing           string   `json:"pricing,omitempty"`
	Harness           string   `json:"harness,omitempty"`
	StatusCode        int      `json:"status_code"`
	Streamed          bool     `json:"streamed"`
	Incomplete        bool     `json:"incomplete"`
	HasToolCalls      bool     `json:"has_tool_calls"`
	Conversation      bool     `json:"is_conversation"`
	Parser            string   `json:"parser"`
	ParseError        string   `json:"parse_error,omitempty"`
	InputTokens       *int64   `json:"input_tokens,omitempty"`
	OutputTokens      *int64   `json:"output_tokens,omitempty"`
	CachedInputTokens *int64   `json:"cached_input_tokens,omitempty"`
	ReasoningTokens   *int64   `json:"reasoning_tokens,omitempty"`
	TotalTokens       *int64   `json:"total_tokens,omitempty"`
	CostAmount        *float64 `json:"cost_amount,omitempty"`
	CostSource        string   `json:"cost_source,omitempty"`
}

type RequestDetail struct {
	RequestSummary
	RequestContentType  string   `json:"request_content_type,omitempty"`
	ResponseContentType string   `json:"response_content_type,omitempty"`
	MetadataJSON        string   `json:"metadata_json,omitempty"`
	RequestBody         string   `json:"request_body"`
	ResponseBody        string   `json:"response_body"`
	RawResponseBody     string   `json:"raw_response_body,omitempty"`
	UsageJSON           string   `json:"usage_json,omitempty"`
	CacheReadTokens     *int64   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens    *int64   `json:"cache_write_tokens,omitempty"`
	ProviderCost        *float64 `json:"provider_cost,omitempty"`
	CalculatedCost      *float64 `json:"calculated_cost,omitempty"`
	CostDetailsJSON     string   `json:"cost_details_json,omitempty"`
	PricingSnapshotJSON string   `json:"pricing_snapshot_json,omitempty"`
}

// Facets are the distinct values available for list filters.
type Facets struct {
	Providers []string `json:"providers"`
	Models    []string `json:"models"`
	Harnesses []string `json:"harnesses"`
}

type ListOptions struct {
	Since  *time.Time
	Limit  int
	Offset int
	Sort   string
	Desc   bool

	// Search and quick filters. Empty/nil values do not filter.
	Query        string
	Failed       bool
	Incomplete   *bool
	Streamed     *bool
	HasToolCalls *bool
	Conversation *bool
	Unassigned   *bool
	Provider     string
	Model        string
	Harness      string
	MinDuration  time.Duration

	// Session-level quick filters.
	HasFailures   *bool
	HasIncomplete *bool
}

type RequestForTree struct {
	ID           int64
	StartedAt    string
	Parser       string
	RequestBody  []byte
	ResponseBody []byte
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) initSchema() error {
	for _, stmt := range schema {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}
	}
	return nil
}

func (s *Store) SaveRequest(ctx context.Context, rec *RequestRecord) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Auxiliary traffic never joins a session, even when the client sent a
	// session header. The header value is not kept as a session fact either, so
	// the request cannot appear to belong to a session it is not part of.
	sessionKey, sessionHeader := rec.SessionKey, rec.SessionHeader
	if !rec.Conversation {
		sessionKey, sessionHeader = "", ""
	}

	var sessionID *int64
	if strings.TrimSpace(sessionKey) != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sessions (session_key) VALUES (?) ON CONFLICT(session_key) DO NOTHING`,
			sessionKey); err != nil {
			return 0, err
		}
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM sessions WHERE session_key = ?`, sessionKey).Scan(&id); err != nil {
			return 0, err
		}
		sessionID = &id
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO requests (
			session_id, session_key, session_header, started_at, completed_at,
			method, local_path, upstream_url, provider, model, status_code,
			request_content_type, response_content_type, metadata_json,
			request_body, response_body, raw_response_body,
			streamed, incomplete, has_tool_calls, conversation, parser, parse_error, usage_json, pricing,
			input_tokens, output_tokens, cached_input_tokens, cache_read_tokens,
			cache_write_tokens, reasoning_tokens, total_tokens,
			provider_cost, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, nullString(sessionKey), nullString(sessionHeader),
		formatTime(rec.StartedAt), nullTime(rec.CompletedAt),
		rec.Method, rec.LocalPath, rec.UpstreamURL, rec.Provider, nullString(rec.Model), rec.StatusCode,
		nullString(rec.RequestContentType), nullString(rec.ResponseContentType), nullString(rec.MetadataJSON),
		rec.RequestBody, rec.ResponseBody, rec.RawResponseBody,
		boolInt(rec.Streamed), boolInt(rec.Incomplete), boolInt(rec.HasToolCalls), boolInt(rec.Conversation), rec.Parser, nullString(rec.ParseError), nullString(rec.UsageJSON),
		nullString(rec.Pricing),
		rec.InputTokens, rec.OutputTokens, rec.CachedInputTokens, rec.CacheReadTokens,
		rec.CacheWriteTokens, rec.ReasoningTokens, rec.TotalTokens,
		rec.ProviderCost, formatTime(time.Now().UTC()),
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// Facets returns the distinct provider, model and harness values present in
// captured requests, for filter suggestions.
func (s *Store) Facets(ctx context.Context) (Facets, error) {
	out := Facets{Providers: []string{}, Models: []string{}, Harnesses: []string{}}

	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT provider FROM requests WHERE provider IS NOT NULL AND provider != '' ORDER BY provider`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			rows.Close()
			return out, err
		}
		out.Providers = append(out.Providers, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx,
		`SELECT DISTINCT model FROM requests WHERE model IS NOT NULL AND model != '' ORDER BY model`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			rows.Close()
			return out, err
		}
		out.Models = append(out.Models, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx,
		`SELECT metadata_json FROM requests WHERE metadata_json IS NOT NULL`)
	if err != nil {
		return out, err
	}
	harnesses := map[string]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return out, err
		}
		if harness := harnessFromMetadata(raw); harness != "" {
			harnesses[harness] = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	for harness := range harnesses {
		out.Harnesses = append(out.Harnesses, harness)
	}
	sort.Strings(out.Harnesses)
	return out, nil
}

func (s *Store) ListSessions(ctx context.Context) ([]SessionSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.session_key,
		       MIN(r.started_at) AS first_seen,
		       MAX(COALESCE(r.completed_at, r.started_at)) AS last_activity,
		       COUNT(r.id),
		       COALESCE(SUM(r.input_tokens), 0),
		       COALESCE(SUM(r.output_tokens), 0),
		       SUM(r.cached_input_tokens),
		       SUM(r.reasoning_tokens),
		       COALESCE(SUM(r.total_tokens), 0),
		       COALESCE(SUM(CASE WHEN r.usage_json IS NULL THEN 0 ELSE 1 END), 0),
		       COALESCE(SUM(CASE WHEN r.status_code >= 400 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN r.incomplete = 1 THEN 1 ELSE 0 END), 0),
		       GROUP_CONCAT(DISTINCT r.provider),
		       GROUP_CONCAT(DISTINCT r.model)
		FROM sessions s
		JOIN requests r ON r.session_id = s.id
		GROUP BY s.id, s.session_key
		ORDER BY last_activity DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionSummary
	for rows.Next() {
		var (
			sess        SessionSummary
			providerCSV sql.NullString
			modelCSV    sql.NullString
			cached      sql.NullInt64
			reasoning   sql.NullInt64
		)
		if err := rows.Scan(&sess.ID, &sess.SessionKey, &sess.FirstSeen, &sess.LastActivity,
			&sess.RequestCount, &sess.InputTokens, &sess.OutputTokens, &cached, &reasoning,
			&sess.TotalTokens, &sess.UsageReportedRequests,
			&sess.FailedRequests, &sess.IncompleteRequests,
			&providerCSV, &modelCSV); err != nil {
			return nil, err
		}
		sess.CachedInputTokens = intPtr(cached)
		sess.ReasoningTokens = intPtr(reasoning)
		sess.Providers = sortedStrings(splitCSV(providerCSV.String))
		sess.Models = sortedStrings(splitCSV(modelCSV.String))
		out = append(out, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	costs, err := s.sessionCosts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		applySessionCost(&out[i], costs[out[i].ID])
	}
	return out, nil
}

// sessionCost aggregates freshly resolved request costs for one session.
type sessionCost struct {
	reported int64
	amount   float64
}

func (c *sessionCost) add(cost Cost) {
	if cost.Amount == nil {
		return
	}
	c.reported++
	c.amount += *cost.Amount
}

// sessionCosts resolves the cost of every captured request and aggregates it
// per session. Calculated costs are recomputed here, so a changed pricing
// configuration is reflected without touching stored data.
func (s *Store) sessionCosts(ctx context.Context) (map[int64]*sessionCost, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT session_id, COALESCE(pricing, ''), COALESCE(model, ''),
		       started_at, COALESCE(usage_json, ''), provider_cost
		FROM requests WHERE session_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]*sessionCost{}
	for rows.Next() {
		var (
			sessionID    int64
			pricingName  string
			model        string
			startedAt    string
			usageJSON    string
			providerCost sql.NullFloat64
		)
		if err := rows.Scan(&sessionID, &pricingName, &model, &startedAt, &usageJSON, &providerCost); err != nil {
			return nil, err
		}
		cost := s.resolveCost(CostFacts{
			Pricing:      pricingName,
			Model:        model,
			CapturedAt:   parseTime(startedAt),
			Usage:        usageFromJSON(usageJSON),
			ProviderCost: floatPtr(providerCost),
		})
		agg := out[sessionID]
		if agg == nil {
			agg = &sessionCost{}
			out[sessionID] = agg
		}
		agg.add(cost)
	}
	return out, rows.Err()
}

// applySessionCost sets the session cost fields from freshly resolved request
// costs. All amounts are treated as USD.
func applySessionCost(sess *SessionSummary, cost *sessionCost) {
	if cost == nil || cost.reported == 0 {
		return
	}
	amount := cost.amount
	sess.CostAmount = &amount
	sess.CostReportedRequests = cost.reported
}

// ListSessionsFiltered filters, sorts and paginates session summaries. Sessions
// are aggregated first, then filtered and sorted in memory.
func (s *Store) ListSessionsFiltered(ctx context.Context, opts ListOptions) ([]SessionSummary, int64, error) {
	all, err := s.ListSessions(ctx)
	if err != nil {
		return nil, 0, err
	}
	filtered := make([]SessionSummary, 0, len(all))
	for _, sess := range all {
		if opts.Since != nil {
			t, err := time.Parse(time.RFC3339Nano, sess.LastActivity)
			if err == nil && t.Before(*opts.Since) {
				continue
			}
		}
		if !matchesSessionFilters(sess, opts) {
			continue
		}
		filtered = append(filtered, sess)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		cmp := 0
		switch opts.Sort {
		case "session_key":
			cmp = strings.Compare(strings.ToLower(a.SessionKey), strings.ToLower(b.SessionKey))
		case "request_count":
			cmp = compareInt64(a.RequestCount, b.RequestCount)
		case "total_tokens":
			cmp = compareInt64(a.TotalTokens, b.TotalTokens)
		case "cost_amount":
			cmp = compareFloatPtr(a.CostAmount, b.CostAmount)
		default:
			at := parseSessionTime(a.LastActivity)
			bt := parseSessionTime(b.LastActivity)
			if at.Before(bt) {
				cmp = -1
			} else if at.After(bt) {
				cmp = 1
			}
		}
		if opts.Desc {
			return cmp > 0
		}
		return cmp < 0
	})

	total := int64(len(filtered))
	start, end := pageBounds(len(filtered), opts.Offset, opts.Limit)
	return filtered[start:end], total, nil
}

func matchesSessionFilters(sess SessionSummary, opts ListOptions) bool {
	if opts.HasFailures != nil && (sess.FailedRequests > 0) != *opts.HasFailures {
		return false
	}
	if opts.HasIncomplete != nil && (sess.IncompleteRequests > 0) != *opts.HasIncomplete {
		return false
	}
	if opts.Provider != "" && !containsFold(sess.Providers, opts.Provider) {
		return false
	}
	if opts.Model != "" && !containsFold(sess.Models, opts.Model) {
		return false
	}
	if needle := strings.ToLower(strings.TrimSpace(opts.Query)); needle != "" {
		haystack := strings.ToLower(strings.Join(
			append([]string{sess.SessionKey}, append(sess.Providers, sess.Models...)...), " "))
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}

func matchesRequestFilters(req RequestSummary, opts ListOptions) bool {
	if opts.Failed && req.StatusCode < 400 {
		return false
	}
	if opts.Incomplete != nil && req.Incomplete != *opts.Incomplete {
		return false
	}
	if opts.Streamed != nil && req.Streamed != *opts.Streamed {
		return false
	}
	if opts.HasToolCalls != nil && req.HasToolCalls != *opts.HasToolCalls {
		return false
	}
	if opts.Conversation != nil && req.Conversation != *opts.Conversation {
		return false
	}
	// Unassigned means an unassigned conversation request, matching the
	// Unassigned group in the Sessions view. Auxiliary traffic is never in a
	// session, so counting it here would make the two filters overlap.
	if opts.Unassigned != nil && (req.SessionID == nil && req.Conversation) != *opts.Unassigned {
		return false
	}
	if opts.Provider != "" && !strings.EqualFold(req.Provider, opts.Provider) {
		return false
	}
	if opts.Model != "" && !strings.EqualFold(req.Model, opts.Model) {
		return false
	}
	if opts.Harness != "" && !strings.EqualFold(req.Harness, opts.Harness) {
		return false
	}
	if opts.MinDuration > 0 {
		duration, ok := requestDuration(req)
		if !ok || duration < opts.MinDuration {
			return false
		}
	}
	if needle := strings.ToLower(strings.TrimSpace(opts.Query)); needle != "" {
		haystack := strings.ToLower(strings.Join([]string{
			fmt.Sprintf("#%d", req.ID),
			fmt.Sprintf("%d", req.ID),
			req.SessionKey,
			req.Provider,
			req.Model,
			req.Harness,
			req.LocalPath,
			req.Method,
			fmt.Sprintf("%d", req.StatusCode),
		}, " "))
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}

func containsFold(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(value, needle) {
			return true
		}
	}
	return false
}

func compareInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func compareFloatPtr(a, b *float64) int {
	av, bv := -1.0, -1.0
	if a != nil {
		av = *a
	}
	if b != nil {
		bv = *b
	}
	switch {
	case av < bv:
		return -1
	case av > bv:
		return 1
	default:
		return 0
	}
}

func parseSessionTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func pageBounds(total, offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 {
		limit = 50
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return offset, end
}

func (s *Store) GetSession(ctx context.Context, id int64) (*SessionSummary, error) {
	costs, err := s.sessionCosts(ctx)
	if err != nil {
		return nil, err
	}
	var (
		sess        SessionSummary
		providerCSV sql.NullString
		modelCSV    sql.NullString
		cached      sql.NullInt64
		reasoning   sql.NullInt64
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT s.id, s.session_key,
		       MIN(r.started_at), MAX(COALESCE(r.completed_at, r.started_at)), COUNT(r.id),
		       COALESCE(SUM(r.input_tokens), 0), COALESCE(SUM(r.output_tokens), 0),
		       SUM(r.cached_input_tokens), SUM(r.reasoning_tokens),
		       COALESCE(SUM(r.total_tokens), 0),
		       COALESCE(SUM(CASE WHEN r.usage_json IS NULL THEN 0 ELSE 1 END), 0),
		       COALESCE(SUM(CASE WHEN r.status_code >= 400 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN r.incomplete = 1 THEN 1 ELSE 0 END), 0),
		       GROUP_CONCAT(DISTINCT r.provider), GROUP_CONCAT(DISTINCT r.model)
		FROM sessions s JOIN requests r ON r.session_id = s.id
		WHERE s.id = ?
		GROUP BY s.id, s.session_key`, id).Scan(
		&sess.ID, &sess.SessionKey, &sess.FirstSeen, &sess.LastActivity, &sess.RequestCount,
		&sess.InputTokens, &sess.OutputTokens, &cached, &reasoning, &sess.TotalTokens,
		&sess.UsageReportedRequests, &sess.FailedRequests, &sess.IncompleteRequests,
		&providerCSV, &modelCSV)
	if err != nil {
		return nil, err
	}
	sess.CachedInputTokens = intPtr(cached)
	sess.ReasoningTokens = intPtr(reasoning)
	sess.Providers = sortedStrings(splitCSV(providerCSV.String))
	sess.Models = sortedStrings(splitCSV(modelCSV.String))
	applySessionCost(&sess, costs[id])
	return &sess, nil
}

const requestSummarySelect = `
	SELECT id, session_id, COALESCE(session_key, ''), COALESCE(session_header, ''),
	       started_at, COALESCE(completed_at, ''), method, local_path, upstream_url,
	       COALESCE(provider, ''), COALESCE(model, ''), status_code,
	       streamed, incomplete, has_tool_calls, conversation, parser, COALESCE(parse_error, ''),
	       input_tokens, output_tokens, cached_input_tokens, reasoning_tokens, total_tokens,
	       COALESCE(pricing, ''), provider_cost, COALESCE(usage_json, ''),
	       COALESCE(metadata_json, '')
	FROM requests`

func (s *Store) ListRequests(ctx context.Context, sessionID int64) ([]RequestSummary, error) {
	return s.queryRequestSummaries(ctx,
		requestSummarySelect+` WHERE session_id = ? ORDER BY started_at ASC, id ASC`, sessionID)
}

// ListAllRequests returns every captured request, independent of session
// grouping. Newest requests come first.
func (s *Store) ListAllRequests(ctx context.Context) ([]RequestSummary, error) {
	return s.queryRequestSummaries(ctx, requestSummarySelect+` ORDER BY started_at DESC, id DESC`)
}

// ListAllRequestsFiltered filters, sorts and paginates request summaries.
// Sorting happens after costs are resolved, so cost ordering always reflects
// the active pricing configuration.
func (s *Store) ListAllRequestsFiltered(ctx context.Context, opts ListOptions) ([]RequestSummary, int64, error) {
	where := ""
	var args []any
	if opts.Since != nil {
		where = ` WHERE started_at >= ?`
		args = append(args, formatTime(*opts.Since))
	}
	requests, err := s.queryRequestSummaries(ctx, requestSummarySelect+where+` ORDER BY started_at ASC, id ASC`, args...)
	if err != nil {
		return nil, 0, err
	}

	filtered := requests[:0]
	for _, req := range requests {
		if matchesRequestFilters(req, opts) {
			filtered = append(filtered, req)
		}
	}
	requests = filtered

	sort.SliceStable(requests, func(i, j int) bool {
		cmp := compareRequestSummaries(requests[i], requests[j], opts.Sort)
		if opts.Desc {
			return cmp > 0
		}
		return cmp < 0
	})

	total := int64(len(requests))
	start, end := pageBounds(len(requests), opts.Offset, opts.Limit)
	return requests[start:end], total, nil
}

func requestDuration(req RequestSummary) (time.Duration, bool) {
	if req.CompletedAt == "" {
		return 0, false
	}
	start, end := parseTime(req.StartedAt), parseTime(req.CompletedAt)
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0, false
	}
	return end.Sub(start), true
}

func compareRequestSummaries(a, b RequestSummary, sortKey string) int {
	switch sortKey {
	case "id":
		return compareInt64(a.ID, b.ID)
	case "duration":
		ad, aok := requestDuration(a)
		bd, bok := requestDuration(b)
		switch {
		case !aok && !bok:
			return 0
		case !aok:
			return -1
		case !bok:
			return 1
		case ad < bd:
			return -1
		case ad > bd:
			return 1
		default:
			return 0
		}
	case "status_code":
		return compareInt64(int64(a.StatusCode), int64(b.StatusCode))
	case "total_tokens":
		return compareInt64Ptr(a.TotalTokens, b.TotalTokens)
	case "cost_amount":
		return compareFloatPtr(a.CostAmount, b.CostAmount)
	default:
		at, bt := parseTime(a.StartedAt), parseTime(b.StartedAt)
		switch {
		case at.Before(bt):
			return -1
		case at.After(bt):
			return 1
		default:
			return compareInt64(a.ID, b.ID)
		}
	}
}

func compareInt64Ptr(a, b *int64) int {
	av, bv := int64(-1), int64(-1)
	if a != nil {
		av = *a
	}
	if b != nil {
		bv = *b
	}
	return compareInt64(av, bv)
}

func (s *Store) queryRequestSummaries(ctx context.Context, query string, args ...any) ([]RequestSummary, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RequestSummary
	for rows.Next() {
		r, err := s.scanRequestSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ListRequestsForTree(ctx context.Context, sessionID int64) ([]RequestForTree, error) {
	query := `SELECT id, started_at, parser, request_body, response_body FROM requests
		WHERE session_id = ? ORDER BY started_at ASC, id ASC`
	rows, err := s.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RequestForTree
	for rows.Next() {
		var r RequestForTree
		var reqBody, respBody []byte
		if err := rows.Scan(&r.ID, &r.StartedAt, &r.Parser, &reqBody, &respBody); err != nil {
			return nil, err
		}
		r.RequestBody = reqBody
		r.ResponseBody = respBody
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetRequest(ctx context.Context, id int64) (*RequestDetail, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, session_id, COALESCE(session_key, ''), COALESCE(session_header, ''),
		       started_at, COALESCE(completed_at, ''), method, local_path, upstream_url,
		       COALESCE(provider, ''), COALESCE(model, ''), status_code,
		       streamed, incomplete, has_tool_calls, conversation, parser, COALESCE(parse_error, ''),
		       input_tokens, output_tokens, cached_input_tokens, reasoning_tokens, total_tokens,
		       COALESCE(pricing, ''), provider_cost,
		       COALESCE(request_content_type, ''), COALESCE(response_content_type, ''),
		       COALESCE(metadata_json, ''), request_body, response_body,
		       COALESCE(raw_response_body, ''), COALESCE(usage_json, ''),
		       cache_read_tokens, cache_write_tokens
		FROM requests WHERE id = ?`, id)

	var (
		d                 RequestDetail
		sessionID         sql.NullInt64
		reqBody, respBody []byte
		rawRespBody       []byte
		inputTokens       sql.NullInt64
		outputTokens      sql.NullInt64
		totalTokens       sql.NullInt64
		cachedInput       sql.NullInt64
		cacheRead         sql.NullInt64
		cacheWrite        sql.NullInt64
		reasoning         sql.NullInt64
		providerCost      sql.NullFloat64
	)
	err := row.Scan(
		&d.ID, &sessionID, &d.SessionKey, &d.SessionHeader,
		&d.StartedAt, &d.CompletedAt, &d.Method, &d.LocalPath, &d.UpstreamURL,
		&d.Provider, &d.Model, &d.StatusCode,
		&d.Streamed, &d.Incomplete, &d.HasToolCalls, &d.Conversation, &d.Parser, &d.ParseError,
		&inputTokens, &outputTokens, &cachedInput, &reasoning, &totalTokens,
		&d.Pricing, &providerCost,
		&d.RequestContentType, &d.ResponseContentType,
		&d.MetadataJSON, &reqBody, &respBody,
		&rawRespBody, &d.UsageJSON,
		&cacheRead, &cacheWrite,
	)
	if err != nil {
		return nil, err
	}
	if sessionID.Valid {
		v := sessionID.Int64
		d.SessionID = &v
	}
	d.InputTokens = intPtr(inputTokens)
	d.OutputTokens = intPtr(outputTokens)
	d.TotalTokens = intPtr(totalTokens)
	d.Harness = harnessFromMetadata(d.MetadataJSON)
	d.CachedInputTokens = intPtr(cachedInput)
	d.CacheReadTokens = intPtr(cacheRead)
	d.CacheWriteTokens = intPtr(cacheWrite)
	d.ReasoningTokens = intPtr(reasoning)
	d.ProviderCost = floatPtr(providerCost)
	d.RequestBody = string(reqBody)
	d.ResponseBody = string(respBody)
	d.RawResponseBody = string(rawRespBody)

	cost := s.resolveCost(CostFacts{
		Pricing:      d.Pricing,
		Model:        d.Model,
		CapturedAt:   parseTime(d.StartedAt),
		Usage:        usageFromJSON(d.UsageJSON),
		ProviderCost: d.ProviderCost,
	})
	d.CostAmount = cost.Amount
	d.CostSource = cost.Source
	d.CostDetailsJSON = cost.DetailsJSON
	d.PricingSnapshotJSON = cost.SnapshotJSON
	if cost.Amount != nil && cost.Source != "" && cost.Source != "openrouter_reported" {
		d.CalculatedCost = cost.Amount
	}
	return &d, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanRequestSummary(row rowScanner) (RequestSummary, error) {
	var (
		r            RequestSummary
		sessionID    sql.NullInt64
		inputTokens  sql.NullInt64
		outputTokens sql.NullInt64
		cachedInput  sql.NullInt64
		reasoning    sql.NullInt64
		totalTokens  sql.NullInt64
		providerCost sql.NullFloat64
		usageJSON    string
		metadataJSON sql.NullString
	)
	err := row.Scan(&r.ID, &sessionID, &r.SessionKey, &r.SessionHeader,
		&r.StartedAt, &r.CompletedAt, &r.Method, &r.LocalPath, &r.UpstreamURL,
		&r.Provider, &r.Model, &r.StatusCode,
		&r.Streamed, &r.Incomplete, &r.HasToolCalls, &r.Conversation, &r.Parser, &r.ParseError,
		&inputTokens, &outputTokens, &cachedInput, &reasoning, &totalTokens,
		&r.Pricing, &providerCost, &usageJSON, &metadataJSON)
	if err != nil {
		return r, err
	}
	if sessionID.Valid {
		v := sessionID.Int64
		r.SessionID = &v
	}
	r.InputTokens = intPtr(inputTokens)
	r.OutputTokens = intPtr(outputTokens)
	r.CachedInputTokens = intPtr(cachedInput)
	r.ReasoningTokens = intPtr(reasoning)
	r.TotalTokens = intPtr(totalTokens)
	r.Harness = harnessFromMetadata(metadataJSON.String)
	cost := s.resolveCost(CostFacts{
		Pricing:      r.Pricing,
		Model:        r.Model,
		CapturedAt:   parseTime(r.StartedAt),
		Usage:        usageFromJSON(usageJSON),
		ProviderCost: floatPtr(providerCost),
	})
	r.CostAmount = cost.Amount
	r.CostSource = cost.Source
	return r, nil
}

func parseTime(value string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}

func usageFromJSON(raw string) usage.Usage {
	if strings.TrimSpace(raw) == "" {
		return usage.Usage{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return usage.Usage{}
	}
	return usage.Extract(m)
}

func intPtr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func floatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

func sortedStrings(values []string) []string {
	sort.Strings(values)
	return values
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func harnessFromMetadata(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return ""
	}
	for key, value := range metadata {
		if strings.EqualFold(key, "request.X-Harness-Id") {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	for key, value := range metadata {
		if strings.EqualFold(key, "request.X-Openrouter-Title") {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	for key, value := range metadata {
		if strings.EqualFold(key, "request.User-Agent") {
			if s, ok := value.(string); ok {
				return harnessFromUserAgent(s)
			}
		}
	}
	return ""
}

func harnessFromUserAgent(ua string) string {
	fields := strings.Fields(strings.TrimSpace(ua))
	if len(fields) == 0 {
		return ""
	}
	name := fields[0]
	if i := strings.IndexByte(name, '/'); i > 0 {
		name = name[:i]
	}
	return name
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// formatTime stores timestamps in a fixed-width RFC3339 form. Fixed width is
// required because SQLite compares TEXT lexicographically, and RFC3339Nano
// trims trailing zeros, which breaks chronological ordering.
func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

var ErrNotFound = errors.New("not found")
