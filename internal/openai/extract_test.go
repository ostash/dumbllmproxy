// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package openai

import "testing"

func TestHasToolCalls(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"tool call", `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1"}]}}]}`, true},
		{"empty tool calls", `{"choices":[{"message":{"role":"assistant","tool_calls":[]}}]}`, false},
		{"text only", `{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`, false},
		{"no choices", `{"choices":[]}`, false},
		{"invalid", `not json`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasToolCalls([]byte(tc.body)); got != tc.want {
				t.Fatalf("HasToolCalls() = %v, want %v", got, tc.want)
			}
		})
	}
}
