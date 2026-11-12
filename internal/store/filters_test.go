// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"dumbllmproxy/internal/store"
)

func TestListRequestFilters(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	now := time.Now().UTC()

	records := []*store.RequestRecord{
		{
			SessionKey: "alpha", Conversation: true, StartedAt: now, Method: "POST", Endpoint: "/openrouter/v1/chat/completions",
			Provider: "openrouter", Model: "model-a",
			StatusCode: 200, Streamed: true, HasToolCalls: true,
		},
		{
			SessionKey: "beta", Conversation: true, StartedAt: now.Add(time.Second), Method: "POST", Endpoint: "/deepseek/chat/completions",
			Provider: "deepseek", Model: "model-b",
			StatusCode: 500, Incomplete: true,
		},
		{
			StartedAt: now.Add(2 * time.Second), Method: "GET", Endpoint: "/openrouter/api/v1/models",
			Provider: "openrouter", Model: "",
			StatusCode: 200,
		},
		{
			Conversation: true, StartedAt: now.Add(3 * time.Second), Method: "POST", Endpoint: "/openrouter/v1/chat/completions",
			Provider: "openrouter", Model: "model-c",
			StatusCode: 200,
		},
	}
	for _, rec := range records {
		if _, err := st.SaveRequest(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	all, total, err := st.ListAllRequestsFiltered(ctx, store.ListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(all) != 4 {
		t.Fatalf("expected 4 requests, got %d/%d", len(all), total)
	}

	cases := []struct {
		name string
		opts store.ListOptions
		want int
	}{
		{"failed", store.ListOptions{Failed: true}, 1},
		{"incomplete", store.ListOptions{Incomplete: boolPtr(true)}, 1},
		{"streamed", store.ListOptions{Streamed: boolPtr(true)}, 1},
		{"tool calls", store.ListOptions{HasToolCalls: boolPtr(true)}, 1},
		{"unassigned", store.ListOptions{Unassigned: boolPtr(true)}, 1},
		{"auxiliary", store.ListOptions{Conversation: boolPtr(false)}, 1},
		{"conversation", store.ListOptions{Conversation: boolPtr(true)}, 3},
		{"provider", store.ListOptions{Provider: "openrouter"}, 3},
		{"model", store.ListOptions{Model: "model-b"}, 1},
		{"query model", store.ListOptions{Query: "model-a"}, 1},
		{"query path", store.ListOptions{Query: "/deepseek/"}, 1},
		{"query session", store.ListOptions{Query: "alpha"}, 1},
		{"query id", store.ListOptions{Query: "#2"}, 1},
		{"query status", store.ListOptions{Query: "500"}, 1},
		{"combined", store.ListOptions{Provider: "openrouter", Failed: true}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Limit = 10
			got, total, err := st.ListAllRequestsFiltered(ctx, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want || int(total) != tc.want {
				t.Fatalf("expected %d results, got %d/%d", tc.want, len(got), total)
			}
		})
	}
}

func TestListSessionFilters(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, rec := range []*store.RequestRecord{
		{SessionKey: "alpha", Conversation: true, StartedAt: now, Method: "POST", Endpoint: "/a", Provider: "openrouter", Model: "model-a", StatusCode: 200},
		{SessionKey: "beta", Conversation: true, StartedAt: now, Method: "POST", Endpoint: "/b", Provider: "deepseek", Model: "model-b", StatusCode: 200},
		{Conversation: true, StartedAt: now, Method: "POST", Endpoint: "/c", Provider: "openrouter", StatusCode: 200},
	} {
		if _, err := st.SaveRequest(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name string
		opts store.ListOptions
		want int
	}{
		{"all", store.ListOptions{}, 2},
		{"provider", store.ListOptions{Provider: "openrouter"}, 1},
		{"model", store.ListOptions{Model: "model-b"}, 1},
		{"query", store.ListOptions{Query: "alph"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Limit = 10
			got, total, err := st.ListSessionsFiltered(ctx, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want || int(total) != tc.want {
				t.Fatalf("expected %d sessions, got %d/%d", tc.want, len(got), total)
			}
		})
	}
}

func boolPtr(v bool) *bool { return &v }

func TestRequestDurationFilterAndSort(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, duration := range []time.Duration{2 * time.Second, 40 * time.Second, 10 * time.Second} {
		completed := base.Add(duration)
		if _, err := st.SaveRequest(ctx, &store.RequestRecord{
			SessionKey: "s", StartedAt: base, CompletedAt: &completed,
			Method: "POST", Endpoint: "/x", Provider: "p", Model: "m", StatusCode: 200,
		}); err != nil {
			t.Fatal(err)
		}
		_ = i
	}

	slow, total, err := st.ListAllRequestsFiltered(ctx, store.ListOptions{
		MinDuration: 30 * time.Second, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(slow) != 1 {
		t.Fatalf("expected one slow request, got %d/%d", len(slow), total)
	}

	sorted, _, err := st.ListAllRequestsFiltered(ctx, store.ListOptions{
		Sort: "duration", Desc: true, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sorted) != 3 {
		t.Fatalf("expected three requests, got %d", len(sorted))
	}
	if d, _ := requestDurationForTest(sorted[0]); d != 40*time.Second {
		t.Fatalf("expected slowest first, got %v", d)
	}
}

func requestDurationForTest(req store.RequestSummary) (time.Duration, bool) {
	if req.CompletedAt == "" {
		return 0, false
	}
	start, err := time.Parse(time.RFC3339Nano, req.StartedAt)
	if err != nil {
		return 0, false
	}
	end, err := time.Parse(time.RFC3339Nano, req.CompletedAt)
	if err != nil {
		return 0, false
	}
	return end.Sub(start), true
}

func TestSessionFailureCountsAndFilters(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, rec := range []*store.RequestRecord{
		{SessionKey: "bad", Conversation: true, StartedAt: now, Method: "POST", Endpoint: "/a", Provider: "p", StatusCode: 500},
		{SessionKey: "bad", Conversation: true, StartedAt: now, Method: "POST", Endpoint: "/b", Provider: "p", StatusCode: 200, Incomplete: true},
		{SessionKey: "good", Conversation: true, StartedAt: now, Method: "POST", Endpoint: "/c", Provider: "p", StatusCode: 200},
	} {
		if _, err := st.SaveRequest(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	sessions, err := st.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]store.SessionSummary{}
	for _, sess := range sessions {
		byKey[sess.SessionKey] = sess
	}
	if byKey["bad"].FailedRequests != 1 || byKey["bad"].IncompleteRequests != 1 {
		t.Fatalf("unexpected bad session counts: %#v", byKey["bad"])
	}
	if byKey["good"].FailedRequests != 0 || byKey["good"].IncompleteRequests != 0 {
		t.Fatalf("unexpected good session counts: %#v", byKey["good"])
	}

	yes := true
	failed, total, err := st.ListSessionsFiltered(ctx, store.ListOptions{HasFailures: &yes, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(failed) != 1 || failed[0].SessionKey != "bad" {
		t.Fatalf("unexpected failed filter result: %#v", failed)
	}
	incomplete, total, err := st.ListSessionsFiltered(ctx, store.ListOptions{HasIncomplete: &yes, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(incomplete) != 1 || incomplete[0].SessionKey != "bad" {
		t.Fatalf("unexpected incomplete filter result: %#v", incomplete)
	}
}

func TestFacetsAndHarnessFilter(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, rec := range []*store.RequestRecord{
		{SessionKey: "s1", StartedAt: now, Method: "POST", Endpoint: "/a", Provider: "openrouter", Model: "model-b", StatusCode: 200, MetadataJSON: `{"request.X-Harness-Id":"pi"}`},
		{SessionKey: "s2", StartedAt: now, Method: "POST", Endpoint: "/b", Provider: "deepseek", Model: "model-a", StatusCode: 200, MetadataJSON: `{"request.X-Harness-Id":"claude"}`},
	} {
		if _, err := st.SaveRequest(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	facets, err := st.Facets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(facets.Providers) != 2 || facets.Providers[0] != "deepseek" || facets.Providers[1] != "openrouter" {
		t.Fatalf("unexpected providers: %#v", facets.Providers)
	}
	if len(facets.Models) != 2 || facets.Models[0] != "model-a" {
		t.Fatalf("unexpected models: %#v", facets.Models)
	}
	if len(facets.Harnesses) != 2 || facets.Harnesses[0] != "claude" || facets.Harnesses[1] != "pi" {
		t.Fatalf("unexpected harnesses: %#v", facets.Harnesses)
	}

	requests, total, err := st.ListAllRequestsFiltered(ctx, store.ListOptions{Harness: "pi", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(requests) != 1 || requests[0].Provider != "openrouter" {
		t.Fatalf("unexpected harness filter result: %#v", requests)
	}
}

func TestSessionProvidersAreSorted(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, provider := range []string{"zeta", "alpha", "middle"} {
		if _, err := st.SaveRequest(ctx, &store.RequestRecord{
			SessionKey: "s", Conversation: true, StartedAt: now, Method: "POST", Endpoint: "/x",
			Provider: provider, StatusCode: 200,
		}); err != nil {
			t.Fatal(err)
		}
	}
	sessions, err := st.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	want := []string{"alpha", "middle", "zeta"}
	if len(sessions[0].Providers) != 3 {
		t.Fatalf("unexpected providers: %#v", sessions[0].Providers)
	}
	for i, value := range want {
		if sessions[0].Providers[i] != value {
			t.Fatalf("providers not sorted: %#v", sessions[0].Providers)
		}
	}
}

func TestAuxiliaryRequestsAreNotSessioned(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	now := time.Now().UTC()

	// A conversation request with a session header joins the session.
	if _, err := st.SaveRequest(ctx, &store.RequestRecord{
		SessionKey: "s", SessionHeader: "X-Session-Id", Conversation: true,
		StartedAt: now, Method: "POST", Endpoint: "/v1/chat/completions",
		Provider: "p", StatusCode: 200,
	}); err != nil {
		t.Fatal(err)
	}
	// Auxiliary traffic with the same header must not join it.
	if _, err := st.SaveRequest(ctx, &store.RequestRecord{
		SessionKey: "s", SessionHeader: "X-Session-Id", Conversation: false,
		StartedAt: now, Method: "GET", Endpoint: "/api/v1/models",
		Provider: "p", StatusCode: 200,
	}); err != nil {
		t.Fatal(err)
	}
	// Auxiliary traffic without a header must not create the unassigned group.
	if _, err := st.SaveRequest(ctx, &store.RequestRecord{
		Conversation: false, StartedAt: now, Method: "GET", Endpoint: "/api/v1/presets",
		Provider: "p", StatusCode: 200,
	}); err != nil {
		t.Fatal(err)
	}

	sessions, err := st.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %#v", sessions)
	}
	if sessions[0].SessionKey != "s" || sessions[0].RequestCount != 1 {
		t.Fatalf("unexpected session: %#v", sessions[0])
	}

	all, err := st.ListAllRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected three captured requests, got %d", len(all))
	}
	for _, req := range all {
		if req.Method == "GET" && (req.SessionID != nil || req.SessionKey != "") {
			t.Fatalf("auxiliary request joined a session: %#v", req)
		}
	}

	aux := false
	auxiliary, total, err := st.ListAllRequestsFiltered(ctx, store.ListOptions{Conversation: &aux, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(auxiliary) != 2 {
		t.Fatalf("expected two auxiliary requests, got %d/%d", len(auxiliary), total)
	}

	// There is no unassigned pseudo-session: unassigned requests are only
	// reachable through the request list.
	if _, err := st.GetSession(ctx, 0); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected no unassigned session, got %v", err)
	}
}
