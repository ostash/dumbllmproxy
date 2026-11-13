// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestHarnessFromMetadata(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"explicit", `{"request.X-Harness-Id":"pi","request.User-Agent":"curl/8.0"}`, "pi"},
		{"openrouter-title", `{"request.X-Openrouter-Title":"Pi Coding Agent","request.User-Agent":"curl/8.0"}`, "Pi Coding Agent"},
		{"user-agent", `{"request.User-Agent":"pi/1.5.0 (linux; node/v26.12.1; x64)"}`, "pi"},
		{"curl", `{"request.User-Agent":"curl/8.23.0"}`, "curl"},
		{"none", `{}`, ""},
		{"invalid", `not json`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := harnessFromMetadata(tc.raw); got != tc.want {
				t.Fatalf("harnessFromMetadata(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSessionSummaryCountsReportedUsageAndCost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	ptr := func(v int64) *int64 { return &v }
	s.SetCostResolver(func(f CostFacts) Cost {
		if f.ProviderCost == nil {
			return Cost{}
		}
		amount := *f.ProviderCost
		return Cost{Amount: &amount, Source: "openrouter_reported"}
	})
	usageJSON := `{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`
	providerCost := 0.25
	records := []*RequestRecord{
		{
			SessionKey: "s1", StartedAt: now, Method: "POST", Endpoint: "/x",
			Provider: "p", StatusCode: 200,
			UsageJSON: usageJSON, InputTokens: ptr(10), OutputTokens: ptr(5),
			TotalTokens: ptr(15), ProviderCost: &providerCost,
		},
		{
			SessionKey: "s1", StartedAt: now.Add(time.Second), Method: "POST", Endpoint: "/y",
			Provider: "p", StatusCode: 200,
		},
	}
	for _, rec := range records {
		if _, err := s.SaveRequest(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	sessions, err := s.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	sess := sessions[0]
	if sess.RequestCount != 2 {
		t.Fatalf("unexpected request count: %d", sess.RequestCount)
	}
	if sess.UsageReportedRequests != 1 || sess.CostReportedRequests != 1 {
		t.Fatalf("unexpected reported counts: usage=%d cost=%d",
			sess.UsageReportedRequests, sess.CostReportedRequests)
	}

	detail, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.UsageReportedRequests != 1 || detail.CostReportedRequests != 1 {
		t.Fatalf("unexpected detail reported counts: usage=%d cost=%d",
			detail.UsageReportedRequests, detail.CostReportedRequests)
	}
}

func TestFormatTimeFixedWidth(t *testing.T) {
	zero := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	half := time.Date(2025, 1, 1, 0, 0, 0, 500_000_000, time.UTC)
	if got := formatTime(zero); got != "2025-01-01T00:00:00.000000000Z" {
		t.Fatalf("unexpected zero-nanos format: %q", got)
	}
	if got := formatTime(half); got != "2025-01-01T00:00:00.500000000Z" {
		t.Fatalf("unexpected half-second format: %q", got)
	}
	if !(formatTime(zero) < formatTime(half)) {
		t.Fatal("fixed-width timestamps must sort chronologically")
	}
}
