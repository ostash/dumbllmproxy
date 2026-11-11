// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package pricing

import (
	"strings"
	"testing"
	"time"

	"dumbllmproxy/internal/config"
)

type stubCalculator struct {
	amount float64
}

func (s stubCalculator) Calculate(Facts) (Result, error) {
	return Result{Amount: s.amount, Source: "stub"}, nil
}

func TestRegistryRegisterAndCalculate(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("Stub", stubCalculator{amount: 1.5}); err != nil {
		t.Fatal(err)
	}
	res, err := r.Calculate("stub", Facts{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Amount != 1.5 || res.Source != "stub" {
		t.Fatalf("unexpected result: %#v", res)
	}
	if got := r.Names(); len(got) != 1 || got[0] != "stub" {
		t.Fatalf("unexpected names: %#v", got)
	}
}

func TestRegistryRejectsDuplicatesAndEmptyNames(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("", stubCalculator{}); err == nil {
		t.Fatal("expected error for empty name")
	}
	if err := r.Register("stub", nil); err == nil {
		t.Fatal("expected error for nil calculator")
	}
	if err := r.Register("stub", stubCalculator{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("STUB", stubCalculator{}); err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestRegistryUnknownAndNone(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"", "none", "missing"} {
		if _, err := r.Calculate(name, Facts{}); err == nil {
			t.Fatalf("expected error for pricing %q", name)
		}
	}
}

func TestDeepSeekImplementsCalculator(t *testing.T) {
	d, err := NewDeepSeek(testPricing())
	if err != nil {
		t.Fatal(err)
	}
	var _ Calculator = d
	res, err := d.Calculate(Facts{
		Model:      "deepseek-chat",
		Usage:      testUsage(),
		CapturedAt: time.Date(2025, 1, 7, 2, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "deepseek_calculated" {
		t.Fatalf("unexpected source: %q", res.Source)
	}
}

func TestDeepSeekDiagnostics(t *testing.T) {
	cfg := config.DeepSeekPricing{
		Unit:             "per_million_tokens",
		ScheduleTimezone: "UTC",
		HolidayTimezone:  "UTC",
		Models: map[string]config.DeepSeekModelPricing{
			"no-catch-all": {
				Rates: []config.PricingRate{{
					Name:       "peak",
					Conditions: &config.RateConditions{Days: []string{"Mon"}},
				}},
			},
			"shadowed": {
				Rates: []config.PricingRate{
					{Name: "broad", Conditions: &config.RateConditions{From: "2025-01-01"}},
					{Name: "narrow", Conditions: &config.RateConditions{From: "2025-02-01"}},
				},
			},
			"bounded": {
				Rates: []config.PricingRate{{
					Name:       "h1",
					Conditions: &config.RateConditions{From: "2025-01-01", To: "2025-06-30"},
				}},
			},
		},
	}
	d, err := NewDeepSeek(cfg)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(d.Diagnostics(), "\n")
	for _, want := range []string{
		"no-catch-all: no catch-all rate",
		"shadowed: rate \"broad\" shadows later rate \"narrow\"",
		"bounded: no rate applies after 2025-06-30",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing diagnostic %q in:\n%s", want, joined)
		}
	}
}

func TestDeepSeekDiagnosticsCleanSchedule(t *testing.T) {
	cfg := config.DeepSeekPricing{
		Unit:             "per_million_tokens",
		ScheduleTimezone: "UTC",
		HolidayTimezone:  "UTC",
		Models: map[string]config.DeepSeekModelPricing{
			"m": {Rates: []config.PricingRate{
				{Name: "peak", Conditions: &config.RateConditions{Days: []string{"Mon"}}},
				{Name: "default"},
			}},
		},
	}
	d, err := NewDeepSeek(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Diagnostics(); len(got) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", got)
	}
}
