// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package pricing

import (
	"testing"
	"time"

	"dumbllmproxy/internal/config"
	"dumbllmproxy/internal/usage"
)

func boolPtr(v bool) *bool { return &v }

func calc(d *DeepSeek, model string, u usage.Usage, at time.Time) (Result, error) {
	return d.Calculate(Facts{Model: model, Usage: u, CapturedAt: at})
}

func peakConditions(from, to string) *config.RateConditions {
	return &config.RateConditions{
		From: from,
		To:   to,
		Days: []string{"Mon", "Tue", "Wed", "Thu", "Fri"},
		TimeWindows: []config.TimeWindow{
			{Start: "01:00", End: "04:00"},
			{Start: "06:00", End: "10:00"},
		},
		Holiday: boolPtr(false),
	}
}

func testPricing() config.DeepSeekPricing {
	return config.DeepSeekPricing{
		Unit:             "per_million_tokens",
		ScheduleTimezone: "UTC",
		HolidayTimezone:  "Asia/Shanghai",
		Holidays:         []string{"2025-01-06"},
		Models: map[string]config.DeepSeekModelPricing{
			"deepseek-chat": {
				Rates: []config.PricingRate{
					{
						Name:           "peak-2025-h1",
						Conditions:     peakConditions("2025-01-01", "2025-06-30"),
						InputCacheHit:  1,
						InputCacheMiss: 2,
						Output:         3,
					},
					{
						Name:           "default-2025-h1",
						Conditions:     &config.RateConditions{From: "2025-01-01", To: "2025-06-30"},
						InputCacheHit:  0.5,
						InputCacheMiss: 1,
						Output:         1.5,
					},
					{
						Name:           "peak-2025-h2",
						Conditions:     peakConditions("2025-07-01", ""),
						InputCacheHit:  10,
						InputCacheMiss: 20,
						Output:         30,
					},
					{
						Name:           "default-2025-h2",
						Conditions:     &config.RateConditions{From: "2025-07-01"},
						InputCacheHit:  5,
						InputCacheMiss: 10,
						Output:         15,
					},
				},
			},
		},
	}
}

func testUsage() usage.Usage {
	return usage.Usage{
		CacheHitTokens:  usage.Int64Ptr(1_000_000),
		CacheMissTokens: usage.Int64Ptr(2_000_000),
		OutputTokens:    usage.Int64Ptr(3_000_000),
	}
}

func TestDeepSeekPeak(t *testing.T) {
	d, err := NewDeepSeek(testPricing())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2025, 1, 7, 2, 0, 0, 0, time.UTC) // Tuesday
	res, err := calc(d, "deepseek-chat", testUsage(), at)
	if err != nil {
		t.Fatal(err)
	}
	if res.Amount != 14 {
		t.Fatalf("expected 14, got %v (%#v)", res.Amount, res.Details)
	}
	if res.Details["rate_name"] != "peak-2025-h1" {
		t.Fatalf("expected old peak rate, got %#v", res.Details["rate_name"])
	}
}

func TestDeepSeekOffPeakWeekend(t *testing.T) {
	d, _ := NewDeepSeek(testPricing())
	res, err := calc(d, "deepseek-chat", testUsage(), time.Date(2025, 1, 11, 2, 0, 0, 0, time.UTC)) // Saturday
	if err != nil {
		t.Fatal(err)
	}
	if res.Amount != 7 {
		t.Fatalf("expected 7, got %v", res.Amount)
	}
	if res.Details["rate_name"] != "default-2025-h1" {
		t.Fatalf("expected old default rate, got %#v", res.Details["rate_name"])
	}
}

func TestDeepSeekHoliday(t *testing.T) {
	d, _ := NewDeepSeek(testPricing())
	// 2025-01-06 02:00 UTC is 10:00 on the holiday date in Asia/Shanghai.
	res, err := calc(d, "deepseek-chat", testUsage(), time.Date(2025, 1, 6, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if res.Amount != 7 {
		t.Fatalf("expected holiday off-peak cost 7, got %v", res.Amount)
	}
	if res.Details["holiday"] != true {
		t.Fatalf("expected holiday=true, got %#v", res.Details["holiday"])
	}
	if res.Details["rate_name"] != "default-2025-h1" {
		t.Fatalf("expected default rate on holiday, got %#v", res.Details["rate_name"])
	}
}

func TestDeepSeekHalfOpenWindow(t *testing.T) {
	d, _ := NewDeepSeek(testPricing())
	res, err := calc(d, "deepseek-chat", testUsage(), time.Date(2025, 1, 7, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if res.Amount != 7 {
		t.Fatalf("expected off-peak at window end, got %v", res.Amount)
	}
}

func TestDeepSeekNewEffectivePeriod(t *testing.T) {
	d, _ := NewDeepSeek(testPricing())
	res, err := calc(d, "deepseek-chat", testUsage(), time.Date(2025, 8, 4, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if res.Amount != 140 {
		t.Fatalf("expected 140, got %v (%#v)", res.Amount, res.Details)
	}
	if res.Details["rate_name"] != "peak-2025-h2" {
		t.Fatalf("expected current peak rate, got %#v", res.Details["rate_name"])
	}
}

func TestDeepSeekToIsInclusive(t *testing.T) {
	d, _ := NewDeepSeek(testPricing())
	old, err := calc(d, "deepseek-chat", testUsage(), time.Date(2025, 6, 30, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if old.Amount != 14 || old.Details["rate_name"] != "peak-2025-h1" {
		t.Fatalf("expected old rate on inclusive to date: %#v", old.Details)
	}
	next, err := calc(d, "deepseek-chat", testUsage(), time.Date(2025, 7, 1, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if next.Amount != 140 || next.Details["rate_name"] != "peak-2025-h2" {
		t.Fatalf("expected new rate after to date: %#v", next.Details)
	}
}

func TestDeepSeekMissingCacheTokens(t *testing.T) {
	d, _ := NewDeepSeek(testPricing())
	u := usage.Usage{OutputTokens: usage.Int64Ptr(10)}
	if _, err := calc(d, "deepseek-chat", u, time.Date(2025, 1, 7, 2, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("expected error for missing cache hit tokens")
	}
}

func TestDeepSeekPerModelRates(t *testing.T) {
	cfg := testPricing()
	cfg.Models["deepseek-reasoner"] = config.DeepSeekModelPricing{
		Rates: []config.PricingRate{
			{
				Name:           "peak",
				Conditions:     peakConditions("2025-01-01", ""),
				InputCacheHit:  2,
				InputCacheMiss: 4,
				Output:         6,
			},
			{
				Name:           "default",
				Conditions:     &config.RateConditions{From: "2025-01-01"},
				InputCacheHit:  1,
				InputCacheMiss: 2,
				Output:         3,
			},
		},
	}
	d, err := NewDeepSeek(cfg)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2025, 1, 7, 2, 0, 0, 0, time.UTC) // Tuesday peak
	chat, err := calc(d, "deepseek-chat", testUsage(), at)
	if err != nil {
		t.Fatal(err)
	}
	reasoner, err := calc(d, "deepseek-reasoner", testUsage(), at)
	if err != nil {
		t.Fatal(err)
	}
	if chat.Amount != 14 {
		t.Fatalf("expected chat cost 14, got %v", chat.Amount)
	}
	if reasoner.Amount != 28 {
		t.Fatalf("expected reasoner cost 28, got %v", reasoner.Amount)
	}
	if chat.Details["model"] != "deepseek-chat" || reasoner.Details["model"] != "deepseek-reasoner" {
		t.Fatalf("unexpected model details: %#v / %#v", chat.Details, reasoner.Details)
	}
	if _, err := calc(d, "unknown-model", testUsage(), at); err == nil {
		t.Fatal("expected error for unconfigured model")
	}
}

func TestDeepSeekFirstMatchingRateWins(t *testing.T) {
	cfg := testPricing()
	cfg.Models["ordered"] = config.DeepSeekModelPricing{
		Rates: []config.PricingRate{
			{
				Name:           "default-first",
				InputCacheHit:  1,
				InputCacheMiss: 2,
				Output:         3,
			},
			{
				Name:           "peak-second",
				Conditions:     peakConditions("", ""),
				InputCacheHit:  10,
				InputCacheMiss: 20,
				Output:         30,
			},
		},
	}
	d, err := NewDeepSeek(cfg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := calc(d, "ordered", testUsage(), time.Date(2025, 1, 7, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if res.Amount != 14 || res.Details["rate_name"] != "default-first" {
		t.Fatalf("expected first matching rate to win: %#v", res.Details)
	}
}

func TestDeepSeekDateConditions(t *testing.T) {
	cfg := testPricing()
	cfg.Models["dates-model"] = config.DeepSeekModelPricing{
		Rates: []config.PricingRate{
			{
				Name:           "special-date",
				Conditions:     &config.RateConditions{Dates: []string{"2025-01-06"}},
				InputCacheHit:  2,
				InputCacheMiss: 4,
				Output:         6,
			},
			{
				Name:           "default",
				InputCacheHit:  1,
				InputCacheMiss: 2,
				Output:         3,
			},
		},
	}
	d, err := NewDeepSeek(cfg)
	if err != nil {
		t.Fatal(err)
	}
	special, err := calc(d, "dates-model", testUsage(), time.Date(2025, 1, 6, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if special.Amount != 28 || special.Details["rate_name"] != "special-date" {
		t.Fatalf("unexpected special date rate: %#v", special.Details)
	}
	normal, err := calc(d, "dates-model", testUsage(), time.Date(2025, 1, 7, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if normal.Amount != 14 || normal.Details["rate_name"] != "default" {
		t.Fatalf("unexpected normal rate: %#v", normal.Details)
	}
}
