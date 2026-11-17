// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package openai

import "testing"

func TestIsConversationRequest(t *testing.T) {
	tests := []struct {
		name    string
		request string
		want    bool
	}{
		{"chat messages", `{"model":"m","messages":[{"role":"user","content":"hi"}]}`, true},
		{"empty messages", `{"model":"m","messages":[]}`, false},
		{"responses input string", `{"model":"m","input":"hi"}`, true},
		{"responses input array", `{"model":"m","input":[{"role":"user"}]}`, true},
		{"legacy prompt", `{"model":"m","prompt":"hi"}`, true},
		{"gemini contents", `{"contents":[{"parts":[]}]}`, true},
		{"empty request", ``, false},
		{"invalid json", `not json`, false},
		{"blank input", `{"input":"   "}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsConversationRequest([]byte(tc.request)); got != tc.want {
				t.Fatalf("IsConversationRequest() = %v, want %v", got, tc.want)
			}
		})
	}
}
