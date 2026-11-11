// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package usage

import "testing"

func TestExtractOpenAIUsage(t *testing.T) {
	u := Extract(map[string]any{
		"prompt_tokens":     float64(100),
		"completion_tokens": float64(20),
		"total_tokens":      float64(120),
		"prompt_tokens_details": map[string]any{
			"cached_tokens":      float64(40),
			"cache_write_tokens": float64(3),
		},
		"completion_tokens_details": map[string]any{
			"reasoning_tokens": float64(8),
		},
		"prompt_cache_hit_tokens":  float64(40),
		"prompt_cache_miss_tokens": float64(60),
		"cost":                     float64(0.005),
	})
	if u.InputTokens == nil || *u.InputTokens != 100 {
		t.Fatalf("unexpected input tokens: %#v", u.InputTokens)
	}
	if u.OutputTokens == nil || *u.OutputTokens != 20 {
		t.Fatalf("unexpected output tokens: %#v", u.OutputTokens)
	}
	if u.TotalTokens == nil || *u.TotalTokens != 120 {
		t.Fatalf("unexpected total tokens: %#v", u.TotalTokens)
	}
	if u.CachedInputTokens == nil || *u.CachedInputTokens != 40 {
		t.Fatalf("unexpected cached tokens: %#v", u.CachedInputTokens)
	}
	if u.CacheWriteTokens == nil || *u.CacheWriteTokens != 3 {
		t.Fatalf("unexpected cache write tokens: %#v", u.CacheWriteTokens)
	}
	if u.CacheHitTokens == nil || *u.CacheHitTokens != 40 {
		t.Fatalf("unexpected cache hit tokens: %#v", u.CacheHitTokens)
	}
	if u.CacheMissTokens == nil || *u.CacheMissTokens != 60 {
		t.Fatalf("unexpected cache miss tokens: %#v", u.CacheMissTokens)
	}
	if u.ReasoningTokens == nil || *u.ReasoningTokens != 8 {
		t.Fatalf("unexpected reasoning tokens: %#v", u.ReasoningTokens)
	}
	if u.ProviderCost == nil || *u.ProviderCost != 0.005 {
		t.Fatalf("unexpected provider cost: %#v", u.ProviderCost)
	}
}

func TestExtractFromResponseMissingUsage(t *testing.T) {
	if _, ok := ExtractFromResponse([]byte(`{"choices":[]}`)); ok {
		t.Fatal("expected no usage")
	}
}

func TestExtractComputesTotal(t *testing.T) {
	u := Extract(map[string]any{"prompt_tokens": float64(3), "completion_tokens": float64(4)})
	if u.TotalTokens == nil || *u.TotalTokens != 7 {
		t.Fatalf("unexpected total: %#v", u.TotalTokens)
	}
}

func TestExtractResponsesReasoningTokens(t *testing.T) {
	u := Extract(map[string]any{
		"input_tokens":  float64(10),
		"output_tokens": float64(7),
		"output_tokens_details": map[string]any{
			"reasoning_tokens": float64(5),
		},
	})
	if u.InputTokens == nil || *u.InputTokens != 10 {
		t.Fatalf("unexpected input tokens: %#v", u.InputTokens)
	}
	if u.OutputTokens == nil || *u.OutputTokens != 7 {
		t.Fatalf("unexpected output tokens: %#v", u.OutputTokens)
	}
	if u.ReasoningTokens == nil || *u.ReasoningTokens != 5 {
		t.Fatalf("unexpected reasoning tokens: %#v", u.ReasoningTokens)
	}
}
