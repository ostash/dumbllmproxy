// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package cost

import (
	"strings"
	"testing"
	"time"

	"dumbllmproxy/internal/pricing"
	"dumbllmproxy/internal/store"
	"dumbllmproxy/internal/usage"
)

type stubCalculator struct{}

func (stubCalculator) Calculate(facts pricing.Facts) (pricing.Result, error) {
	return pricing.Result{
		Amount:   2.5,
		Source:   "stub_calculated",
		Details:  map[string]any{"model": facts.Model},
		Snapshot: map[string]any{"rate": 1},
	}, nil
}

func TestResolvePrefersProviderReportedCost(t *testing.T) {
	registry := pricing.NewRegistry()
	if err := registry.Register("stub", stubCalculator{}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(registry)
	providerCost := 0.75
	got := r.Resolve(store.CostFacts{
		Pricing:      "stub",
		Model:        "m",
		ProviderCost: &providerCost,
	})
	if got.Amount == nil || *got.Amount != 0.75 {
		t.Fatalf("expected provider cost, got %#v", got.Amount)
	}
	if got.Source != "openrouter_reported" {
		t.Fatalf("unexpected source: %q", got.Source)
	}
}

func TestResolveUsesCalculator(t *testing.T) {
	registry := pricing.NewRegistry()
	if err := registry.Register("stub", stubCalculator{}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(registry)
	got := r.Resolve(store.CostFacts{
		Pricing:    "stub",
		Model:      "m",
		CapturedAt: time.Now(),
		Usage:      usage.Usage{},
	})
	if got.Amount == nil || *got.Amount != 2.5 {
		t.Fatalf("expected calculated cost, got %#v", got.Amount)
	}
	if got.Source != "stub_calculated" {
		t.Fatalf("unexpected source: %q", got.Source)
	}
	if !strings.Contains(got.DetailsJSON, `"model":"m"`) {
		t.Fatalf("unexpected details: %s", got.DetailsJSON)
	}
	if got.SnapshotJSON == "" {
		t.Fatal("expected pricing snapshot")
	}
}

func TestResolveUnavailable(t *testing.T) {
	r := NewResolver(pricing.NewRegistry())
	for _, pricingName := range []string{"", "none", "missing"} {
		got := r.Resolve(store.CostFacts{Pricing: pricingName, Model: "m"})
		if got.Amount != nil {
			t.Fatalf("pricing %q: expected unavailable cost, got %v", pricingName, *got.Amount)
		}
		if got.DetailsJSON == "" {
			t.Fatalf("pricing %q: expected an explanation", pricingName)
		}
	}
}
