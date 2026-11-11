// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package openai

import "testing"

func TestIsConversationRequest(t *testing.T) {
	tests := []struct {
		name     string
		request  string
		response string
		want     bool
	}{
		{"chat messages", `{"model":"m","messages":[{"role":"user","content":"hi"}]}`, "", true},
		{"empty messages", `{"model":"m","messages":[]}`, "", false},
		{"responses input string", `{"model":"m","input":"hi"}`, "", true},
		{"responses input array", `{"model":"m","input":[{"role":"user"}]}`, "", true},
		{"legacy prompt", `{"model":"m","prompt":"hi"}`, "", true},
		{"gemini contents", `{"contents":[{"parts":[]}]}`, "", true},
		{"model listing", ``, `{"data":[{"id":"m"}]}`, false},
		{"presets", ``, `{"data":[]}`, false},
		{"response fallback", ``, `{"choices":[{"message":{}}]}`, true},
		{"empty response", ``, ``, false},
		{"invalid json", `not json`, `not json`, false},
		{"blank input", `{"input":"   "}`, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsConversationRequest([]byte(tc.request), []byte(tc.response))
			if got != tc.want {
				t.Fatalf("IsConversationRequest() = %v, want %v", got, tc.want)
			}
		})
	}
}
