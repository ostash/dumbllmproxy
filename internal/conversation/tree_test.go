// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package conversation

import (
	"encoding/json"
	"testing"

	"dumbllmproxy/internal/store"
)

func req(id int64, messages []map[string]any, response map[string]any) store.RequestForTree {
	body, _ := json.Marshal(map[string]any{"messages": messages})
	resp, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"index": 0, "message": response}},
	})
	return store.RequestForTree{
		ID:           id,
		Parser:       "openai_chat",
		RequestBody:  body,
		ResponseBody: resp,
	}
}

func TestBuildTreeBranchesAndOccurrences(t *testing.T) {
	system := map[string]any{"role": "system", "content": "sys"}
	user1 := map[string]any{"role": "user", "content": "one"}
	assistant1 := map[string]any{"role": "assistant", "content": "hi"}
	user2 := map[string]any{"role": "user", "content": "two"}
	user3 := map[string]any{"role": "user", "content": "three"}

	tree := Build([]store.RequestForTree{
		req(1, []map[string]any{system, user1}, assistant1),
		req(2, []map[string]any{system, user1, assistant1, user2}, map[string]any{"role": "assistant", "content": "reply2"}),
		req(3, []map[string]any{system, user1, assistant1, user3}, map[string]any{"role": "assistant", "content": "reply3"}),
	})

	if len(tree.Roots) != 1 {
		t.Fatalf("expected one root, got %d", len(tree.Roots))
	}
	sys := tree.Roots[0]
	if sys.Role != "system" || sys.SourceRequestID != 1 {
		t.Fatalf("unexpected system node: %#v", sys)
	}
	u1 := sys.Children[0]
	if u1.Role != "user" || u1.Content != "one" {
		t.Fatalf("unexpected user node: %#v", u1)
	}
	a1 := u1.Children[0]
	if a1.Role != "assistant" || a1.Content != "hi" {
		t.Fatalf("unexpected assistant node: %#v", a1)
	}
	if a1.SourceRequestID != 1 {
		t.Fatalf("expected source request 1, got %d", a1.SourceRequestID)
	}
	if len(a1.RequestIDs) != 3 || a1.RequestIDs[0] != 1 || a1.RequestIDs[1] != 2 || a1.RequestIDs[2] != 3 {
		t.Fatalf("unexpected occurrences: %#v", a1.RequestIDs)
	}
	if len(a1.Children) != 2 {
		t.Fatalf("expected two branches, got %d", len(a1.Children))
	}
	if a1.Children[0].Content != "two" || a1.Children[0].SourceRequestID != 2 {
		t.Fatalf("unexpected first branch: %#v", a1.Children[0])
	}
	if a1.Children[1].Content != "three" || a1.Children[1].SourceRequestID != 3 {
		t.Fatalf("unexpected second branch: %#v", a1.Children[1])
	}
}

func TestBuildTreeIgnoresReasoningForIdentity(t *testing.T) {
	system := map[string]any{"role": "system", "content": "sys"}
	user := map[string]any{"role": "user", "content": "q"}
	assistantWithReasoning := map[string]any{
		"role": "assistant", "content": "a", "reasoning_content": "because",
	}
	assistantWithoutReasoning := map[string]any{"role": "assistant", "content": "a"}

	tree := Build([]store.RequestForTree{
		req(1, []map[string]any{system, user}, assistantWithReasoning),
		req(2, []map[string]any{system, user, assistantWithoutReasoning}, map[string]any{"role": "assistant", "content": "b"}),
	})
	a := tree.Roots[0].Children[0].Children[0]
	if len(a.RequestIDs) != 2 {
		t.Fatalf("expected response and history to merge, got %#v", a.RequestIDs)
	}
}

func TestBuildTreeMergesEmptyAndNullAssistantContent(t *testing.T) {
	system := map[string]any{"role": "system", "content": "sys"}
	user := map[string]any{"role": "user", "content": "q"}
	responseToolCalls := []any{map[string]any{
		"id": "call_1", "type": "function",
		"function": map[string]any{"name": "lookup", "arguments": `{"x": 1}`},
	}}
	historyToolCalls := []any{map[string]any{
		"id": "call_1", "type": "function",
		"function": map[string]any{"name": "lookup", "arguments": `{"x":1}`},
	}}
	responseAssistant := map[string]any{
		"role": "assistant", "content": "\n\n\n", "tool_calls": responseToolCalls,
		"reasoning": "thinking", "reasoning_details": []any{map[string]any{"type": "reasoning.text", "text": "thinking"}},
	}
	historyAssistant := map[string]any{
		"role": "assistant", "content": nil, "tool_calls": historyToolCalls,
	}
	toolResult := map[string]any{"role": "tool", "content": "ok", "tool_call_id": "call_1"}
	finalAssistant := map[string]any{"role": "assistant", "content": "done"}

	tree := Build([]store.RequestForTree{
		req(1, []map[string]any{system, user}, responseAssistant),
		req(2, []map[string]any{system, user, historyAssistant, toolResult}, finalAssistant),
	})

	userNode := tree.Roots[0].Children[0]
	if len(userNode.Children) != 1 {
		t.Fatalf("expected one assistant node, got %d", len(userNode.Children))
	}
	assistantNode := userNode.Children[0]
	if len(assistantNode.RequestIDs) != 2 {
		t.Fatalf("expected response and history to merge, got %#v", assistantNode.RequestIDs)
	}
	if len(assistantNode.Children) != 1 || assistantNode.Children[0].Role != "tool" {
		t.Fatalf("unexpected assistant children: %#v", assistantNode.Children)
	}
}
