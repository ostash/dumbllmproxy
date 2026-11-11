// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package openai

import (
	"encoding/json"
	"strings"

	"dumbllmproxy/internal/usage"
)

// ExtractModel returns the model field from a JSON request body.
func ExtractModel(body []byte) string {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return ""
	}
	model, _ := obj["model"].(string)
	return strings.TrimSpace(model)
}

// ExtractUsage parses a complete JSON response and returns normalized usage.
func ExtractUsage(body []byte) (usage.Usage, bool) {
	return usage.ExtractFromResponse(body)
}

// ExtractResponseMessage returns choices[0].message as raw JSON.
func ExtractResponseMessage(body []byte) (json.RawMessage, bool) {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, false
	}
	choices, ok := obj["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, false
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return nil, false
	}
	msg, ok := choice["message"]
	if !ok {
		return nil, false
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return nil, false
	}
	return raw, true
}

// ExtractMessages returns the request messages array as raw JSON values.
func ExtractMessages(body []byte) ([]json.RawMessage, bool) {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, false
	}
	msgs, ok := obj["messages"].([]any)
	if !ok {
		return nil, false
	}
	out := make([]json.RawMessage, 0, len(msgs))
	for _, m := range msgs {
		raw, err := json.Marshal(m)
		if err != nil {
			return nil, false
		}
		out = append(out, raw)
	}
	return out, true
}

// HasToolCalls reports whether a chat completion response contains at least one
// tool call in its first choice message.
func HasToolCalls(body []byte) bool {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return false
	}
	choices, ok := obj["choices"].([]any)
	if !ok || len(choices) == 0 {
		return false
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return false
	}
	msg, ok := choice["message"].(map[string]any)
	if !ok {
		return false
	}
	calls, ok := msg["tool_calls"].([]any)
	return ok && len(calls) > 0
}
