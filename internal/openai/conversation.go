// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package openai

import (
	"encoding/json"
	"strings"
)

// conversationInputKeys are JSON fields that carry the input of a model
// generation. A request with a non-empty value under any of them is treated as
// a conversation request.
var conversationInputKeys = []string{"messages", "input", "prompt", "contents"}

// IsConversationRequest reports whether a request body is a model generation
// rather than auxiliary traffic such as model listings, presets or health
// checks. Only conversation requests are captured; auxiliary traffic is
// forwarded without buffering and discarded.
func IsConversationRequest(requestBody []byte) bool {
	return hasNonEmptyField(requestBody, conversationInputKeys)
}

func hasNonEmptyField(body []byte, keys []string) bool {
	if len(body) == 0 {
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return false
	}
	for _, key := range keys {
		if nonEmptyValue(obj[key]) {
			return true
		}
	}
	return false
}

func nonEmptyValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return true
	}
}
