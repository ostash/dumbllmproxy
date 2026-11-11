// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"dumbllmproxy/internal/config"
	"dumbllmproxy/internal/cost"
	"dumbllmproxy/internal/pricing"
	"dumbllmproxy/internal/store"
)

const deepSeekUsageJSON = `{"prompt_tokens":3000000,"completion_tokens":3000000,"total_tokens":6000000,"prompt_cache_hit_tokens":1000000,"prompt_cache_miss_tokens":2000000}`

func deepSeekResolver(t *testing.T, hit, miss, output float64) store.CostResolver {
	t.Helper()
	ds, err := pricing.NewDeepSeek(config.DeepSeekPricing{
		Unit:             "per_million_tokens",
		ScheduleTimezone: "UTC",
		HolidayTimezone:  "UTC",
		Models: map[string]config.DeepSeekModelPricing{
			"deepseek-chat": {Rates: []config.PricingRate{{
				Name:           "default",
				InputCacheHit:  hit,
				InputCacheMiss: miss,
				Output:         output,
			}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := pricing.NewRegistry()
	if err := registry.Register("deepseek", ds); err != nil {
		t.Fatal(err)
	}
	return cost.NewResolver(registry).Resolve
}

func saveDeepSeekRequest(t *testing.T, st *store.Store, sessionKey string, startedAt time.Time) int64 {
	t.Helper()
	id, err := st.SaveRequest(context.Background(), &store.RequestRecord{
		SessionKey:  sessionKey,
		StartedAt:   startedAt,
		Method:      "POST",
		LocalPath:   "/deepseek/chat/completions",
		UpstreamURL: "https://api.deepseek.com/chat/completions",
		Provider:    "deepseek",
		Model:       "deepseek-chat",
		Pricing:     "deepseek",
		StatusCode:  200,
		Parser:      "openai_chat",
		UsageJSON:   deepSeekUsageJSON,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCalculatedCostIsRecomputedOnRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	started := time.Date(2025, 1, 7, 2, 0, 0, 0, time.UTC)
	id := saveDeepSeekRequest(t, st, "s1", started)

	st.SetCostResolver(deepSeekResolver(t, 1, 2, 3))
	detail, err := st.GetRequest(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.CostAmount == nil || *detail.CostAmount != 14 {
		t.Fatalf("expected cost 14, got %#v", detail.CostAmount)
	}
	if detail.CostSource != "deepseek_calculated" {
		t.Fatalf("unexpected source: %q", detail.CostSource)
	}
	if detail.CalculatedCost == nil || *detail.CalculatedCost != 14 {
		t.Fatalf("expected calculated cost 14, got %#v", detail.CalculatedCost)
	}
	if detail.CostDetailsJSON == "" || detail.PricingSnapshotJSON == "" {
		t.Fatal("expected cost explanation and pricing snapshot")
	}

	sessions, err := st.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].CostAmount == nil || *sessions[0].CostAmount != 14 {
		t.Fatalf("unexpected session cost: %#v", sessions)
	}

	// A changed pricing configuration must correct previously captured data.
	st.SetCostResolver(deepSeekResolver(t, 10, 20, 30))
	detail, err = st.GetRequest(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.CostAmount == nil || *detail.CostAmount != 140 {
		t.Fatalf("expected recalculated cost 140, got %#v", detail.CostAmount)
	}
	sessions, err = st.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sessions[0].CostAmount == nil || *sessions[0].CostAmount != 140 {
		t.Fatalf("expected recalculated session cost 140, got %#v", sessions[0].CostAmount)
	}
	requests, err := st.ListRequests(ctx, sessions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].CostAmount == nil || *requests[0].CostAmount != 140 {
		t.Fatalf("expected recalculated request list cost 140, got %#v", requests)
	}
}

func TestSessionCostSumsResolvedAmounts(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	for i, model := range []string{"usd-model", "eur-model"} {
		if _, err := st.SaveRequest(ctx, &store.RequestRecord{
			SessionKey:  "mixed",
			StartedAt:   now.Add(time.Duration(i) * time.Second),
			Method:      "POST",
			LocalPath:   "/x",
			UpstreamURL: "https://example.com/x",
			Provider:    "p",
			Model:       model,
			Pricing:     "stub",
			StatusCode:  200,
		}); err != nil {
			t.Fatal(err)
		}
	}
	st.SetCostResolver(func(f store.CostFacts) store.Cost {
		amount := 1.0
		if f.Model == "eur-model" {
			amount = 2.0
		}
		return store.Cost{Amount: &amount, Source: "stub"}
	})

	sessions, err := st.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	sess := sessions[0]
	if sess.CostAmount == nil || *sess.CostAmount != 3 {
		t.Fatalf("expected summed cost 3, got %#v", sess.CostAmount)
	}
	if sess.CostReportedRequests != 2 {
		t.Fatalf("unexpected reported cost count: %d", sess.CostReportedRequests)
	}
}

func TestRequestCostSortingUsesFreshCosts(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	for i, model := range []string{"a", "b"} {
		if _, err := st.SaveRequest(ctx, &store.RequestRecord{
			SessionKey:  "sort",
			StartedAt:   now.Add(time.Duration(i) * time.Second),
			Method:      "POST",
			LocalPath:   "/x",
			UpstreamURL: "https://example.com/x",
			Provider:    "p",
			Model:       model,
			Pricing:     "stub",
			StatusCode:  200,
		}); err != nil {
			t.Fatal(err)
		}
	}

	amounts := map[string]float64{"a": 1, "b": 2}
	st.SetCostResolver(func(f store.CostFacts) store.Cost {
		amount := amounts[f.Model]
		return store.Cost{Amount: &amount, Source: "stub"}
	})
	opts := store.ListOptions{Sort: "cost_amount", Desc: true, Limit: 10}
	requests, _, err := st.ListAllRequestsFiltered(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || requests[0].Model != "b" {
		t.Fatalf("unexpected order: %#v", requests)
	}

	amounts["a"], amounts["b"] = 5, 1
	requests, _, err = st.ListAllRequestsFiltered(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || requests[0].Model != "a" {
		t.Fatalf("sorting did not use recalculated costs: %#v", requests)
	}
}

func TestSessionLastActivityUsesChronologicalOrder(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, started := range []time.Time{base, base.Add(500 * time.Millisecond)} {
		if _, err := st.SaveRequest(ctx, &store.RequestRecord{
			SessionKey: "s", StartedAt: started, Method: "POST", LocalPath: "/x",
			UpstreamURL: "https://example.com/x", Provider: "p", StatusCode: 200,
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
	if sessions[0].LastActivity != "2025-01-01T00:00:00.500000000Z" {
		t.Fatalf("unexpected last activity: %q", sessions[0].LastActivity)
	}
	if sessions[0].FirstSeen != "2025-01-01T00:00:00.000000000Z" {
		t.Fatalf("unexpected first seen: %q", sessions[0].FirstSeen)
	}
}

func TestSinceFilterUsesChronologicalOrder(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	started := time.Date(2025, 1, 1, 0, 0, 0, 123_456_789, time.UTC)
	if _, err := st.SaveRequest(ctx, &store.RequestRecord{
		SessionKey: "s", StartedAt: started, Method: "POST", LocalPath: "/x",
		UpstreamURL: "https://example.com/x", Provider: "p", StatusCode: 200,
	}); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2025, 1, 1, 0, 0, 0, 123_000_000, time.UTC)
	requests, total, err := st.ListAllRequestsFiltered(ctx, store.ListOptions{Since: &since, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(requests) != 1 {
		t.Fatalf("request after since was excluded: total=%d len=%d", total, len(requests))
	}
}
