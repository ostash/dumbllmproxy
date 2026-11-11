// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package usage

import (
	"encoding/json"
	"math"
	"strconv"
)

// Usage is the normalized token/cost information extracted from an LLM
// response. Pointer fields distinguish "not reported" from zero.
type Usage struct {
	InputTokens       *int64   `json:"input_tokens,omitempty"`
	OutputTokens      *int64   `json:"output_tokens,omitempty"`
	CachedInputTokens *int64   `json:"cached_input_tokens,omitempty"`
	CacheReadTokens   *int64   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens  *int64   `json:"cache_write_tokens,omitempty"`
	ReasoningTokens   *int64   `json:"reasoning_tokens,omitempty"`
	TotalTokens       *int64   `json:"total_tokens,omitempty"`
	CacheHitTokens    *int64   `json:"cache_hit_tokens,omitempty"`
	CacheMissTokens   *int64   `json:"cache_miss_tokens,omitempty"`
	ProviderCost      *float64 `json:"provider_cost,omitempty"`
	Raw               any      `json:"raw,omitempty"`
}

// Extract reads a provider usage object into the normalized form. Unknown
// fields are preserved in Raw.
func Extract(raw map[string]any) Usage {
	u := Usage{Raw: raw}
	if raw == nil {
		return u
	}

	u.InputTokens = firstInt(raw, "prompt_tokens", "input_tokens")
	u.OutputTokens = firstInt(raw, "completion_tokens", "output_tokens")
	u.TotalTokens = firstInt(raw, "total_tokens")

	if details, ok := raw["prompt_tokens_details"].(map[string]any); ok {
		u.CachedInputTokens = firstInt(details, "cached_tokens")
		u.CacheWriteTokens = firstInt(details, "cache_write_tokens")
		if u.CacheReadTokens == nil {
			u.CacheReadTokens = firstInt(details, "cache_read_tokens")
		}
	}
	if details, ok := raw["input_tokens_details"].(map[string]any); ok {
		if u.CachedInputTokens == nil {
			u.CachedInputTokens = firstInt(details, "cached_tokens")
		}
		if u.CacheWriteTokens == nil {
			u.CacheWriteTokens = firstInt(details, "cache_write_tokens")
		}
		if u.CacheReadTokens == nil {
			u.CacheReadTokens = firstInt(details, "cache_read_tokens")
		}
	}
	if v := firstInt(raw, "prompt_cache_hit_tokens", "cache_hit_tokens"); v != nil {
		u.CacheHitTokens = v
		if u.CachedInputTokens == nil {
			u.CachedInputTokens = v
		}
	}
	u.CacheMissTokens = firstInt(raw, "prompt_cache_miss_tokens", "cache_miss_tokens")
	if v := firstInt(raw, "cache_read_input_tokens", "cache_read_tokens"); v != nil {
		u.CacheReadTokens = v
	}
	if v := firstInt(raw, "cache_creation_input_tokens", "cache_write_tokens"); v != nil {
		u.CacheWriteTokens = v
	}
	if details, ok := raw["completion_tokens_details"].(map[string]any); ok {
		u.ReasoningTokens = firstInt(details, "reasoning_tokens")
	}
	if details, ok := raw["output_tokens_details"].(map[string]any); ok {
		if u.ReasoningTokens == nil {
			u.ReasoningTokens = firstInt(details, "reasoning_tokens")
		}
	}
	if u.ReasoningTokens == nil {
		u.ReasoningTokens = firstInt(raw, "reasoning_tokens")
	}

	if cost := firstFloat(raw, "cost", "total_cost", "cost_usd"); cost != nil {
		u.ProviderCost = cost
	}

	if u.TotalTokens == nil && u.InputTokens != nil && u.OutputTokens != nil {
		total := *u.InputTokens + *u.OutputTokens
		u.TotalTokens = &total
	}
	return u
}

// ExtractFromResponse parses a complete JSON response and extracts its top-level
// "usage" object, if present.
func ExtractFromResponse(body []byte) (Usage, bool) {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return Usage{}, false
	}
	raw, ok := obj["usage"].(map[string]any)
	if !ok {
		return Usage{}, false
	}
	return Extract(raw), true
}

func firstInt(m map[string]any, keys ...string) *int64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if n := toInt(v); n != nil {
				return n
			}
		}
	}
	return nil
}

func firstFloat(m map[string]any, keys ...string) *float64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if f := toFloat(v); f != nil {
				return f
			}
		}
	}
	return nil
}

func toInt(v any) *int64 {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil
		}
		i := int64(n)
		return &i
	case int:
		i := int64(n)
		return &i
	case int64:
		i := n
		return &i
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return nil
		}
		return &i
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return nil
		}
		return &i
	default:
		return nil
	}
}

func toFloat(v any) *float64 {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil
		}
		return &n
	case int:
		f := float64(n)
		return &f
	case int64:
		f := float64(n)
		return &f
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return nil
		}
		return &f
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return nil
		}
		return &f
	default:
		return nil
	}
}

func Int64Ptr(v int64) *int64       { return &v }
func Float64Ptr(v float64) *float64 { return &v }
