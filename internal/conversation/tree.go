// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package conversation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"dumbllmproxy/internal/openai"
	"dumbllmproxy/internal/store"
)

// Node is a message in a reconstructed conversation. The same logical message
// can appear in several HTTP requests; RequestIDs records all occurrences while
// SourceRequestID points at the request that first introduced it.
type Node struct {
	ID              string          `json:"id"`
	ParentID        string          `json:"parent_id,omitempty"`
	Role            string          `json:"role"`
	Content         string          `json:"content,omitempty"`
	Reasoning       string          `json:"reasoning,omitempty"`
	ToolCalls       json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID      string          `json:"tool_call_id,omitempty"`
	Name            string          `json:"name,omitempty"`
	SourceRequestID int64           `json:"source_request_id"`
	RequestIDs      []int64         `json:"request_ids"`
	Children        []*Node         `json:"children,omitempty"`

	hash string
}

type Tree struct {
	Roots []*Node `json:"roots"`
}

type builder struct {
	nextID int
	roots  []*Node
	// hashes memoizes identity hashes for the duration of one Build. Requests
	// replay the same history, so the same raw message is hashed many times;
	// the map is discarded with the builder.
	hashes map[string]string
}

// Build reconstructs a conversation tree from captured requests. Only requests
// using the openai_chat parser participate; unknown formats remain available in
// the request inspector.
//
// Only completed exchanges contribute. A request that failed (for example a
// 401 or 404) or whose response could not be parsed has no assistant message,
// so adding its prompt would leave a dangling turn that never happened. Such
// requests stay visible in the session's request list and timeline.
func Build(requests []store.RequestForTree) Tree {
	b := &builder{}
	root := &Node{}
	for _, req := range requests {
		if req.Parser != "openai_chat" {
			continue
		}
		response, ok := openai.ExtractResponseMessage(req.ResponseBody)
		if !ok {
			continue
		}
		current := root
		if messages, ok := openai.ExtractMessages(req.RequestBody); ok {
			for _, raw := range messages {
				current = b.findOrCreate(current, raw, req.ID)
				addRequestID(current, req.ID)
			}
		}
		node := b.findOrCreate(current, response, req.ID)
		addRequestID(node, req.ID)
	}
	return Tree{Roots: b.roots}
}

func (b *builder) findOrCreate(parent *Node, raw json.RawMessage, requestID int64) *Node {
	// The map lookup form avoids allocating a string on a hit.
	h, ok := b.hashes[string(raw)]
	if !ok {
		key := string(raw)
		h = identityHash(raw)
		if b.hashes == nil {
			b.hashes = make(map[string]string)
		}
		b.hashes[key] = h
	}
	for _, child := range parent.Children {
		if child.hash == h {
			return child
		}
	}
	b.nextID++
	node := &Node{
		ID:              fmt.Sprintf("n%d", b.nextID),
		Role:            "unknown",
		SourceRequestID: requestID,
		RequestIDs:      []int64{requestID},
		hash:            h,
	}
	if parent.ID != "" {
		node.ParentID = parent.ID
	}
	describe(node, raw)
	parent.Children = append(parent.Children, node)
	if parent == nil || parent.ID == "" {
		b.roots = append(b.roots, node)
	}
	return node
}

func addRequestID(node *Node, id int64) {
	// Requests arrive in chronological order, so a repeated request ID can only
	// be the most recently appended one. Checking the tail avoids a scan over
	// every ID a frequently repeated message has accumulated.
	if n := len(node.RequestIDs); n > 0 && node.RequestIDs[n-1] == id {
		return
	}
	node.RequestIDs = append(node.RequestIDs, id)
}

func identityHash(raw json.RawMessage) string {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	// JSON null and an omitted field are equivalent for message identity.
	for k, v := range obj {
		if v == nil {
			delete(obj, k)
		}
	}
	if role, _ := obj["role"].(string); role == "assistant" {
		// Response reconstruction may add reasoning fields and use empty
		// content/tool metadata where the next request omits them. These should
		// not create a spurious branch.
		delete(obj, "reasoning")
		delete(obj, "reasoning_content")
		delete(obj, "reasoning_details")
		delete(obj, "annotations")
		delete(obj, "audio")
		delete(obj, "service_tier")
		for _, key := range []string{"content", "tool_calls", "function_call", "tool_call_id", "name", "refusal"} {
			if isEmptyValue(obj[key]) {
				delete(obj, key)
			}
		}
		normalizeToolArguments(obj)
	}
	canonical, err := json.Marshal(obj)
	if err != nil {
		canonical = raw
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func normalizeToolArguments(obj map[string]any) {
	if calls, ok := obj["tool_calls"].([]any); ok {
		for _, rawCall := range calls {
			call, ok := rawCall.(map[string]any)
			if !ok {
				continue
			}
			if fn, ok := call["function"].(map[string]any); ok {
				normalizeJSONStringField(fn, "arguments")
			}
		}
	}
	if fn, ok := obj["function_call"].(map[string]any); ok {
		normalizeJSONStringField(fn, "arguments")
	}
}

func normalizeJSONStringField(m map[string]any, key string) {
	s, ok := m[key].(string)
	if !ok {
		return
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return
	}
	canonical, err := json.Marshal(v)
	if err != nil {
		return
	}
	m[key] = string(canonical)
}

func isEmptyValue(v any) bool {
	switch c := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(c) == ""
	case []any:
		return len(c) == 0
	default:
		return false
	}
}

func describe(node *Node, raw json.RawMessage) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		node.Content = string(raw)
		return
	}
	if role, ok := obj["role"].(string); ok && role != "" {
		node.Role = role
	}
	node.Content = contentText(obj["content"])
	node.Reasoning = firstString(obj, "reasoning_content", "reasoning")
	if node.Reasoning == "" {
		node.Reasoning = reasoningDetailsText(obj["reasoning_details"])
	}
	if v, ok := obj["tool_calls"]; ok && v != nil {
		if rawCalls, err := json.Marshal(v); err == nil {
			node.ToolCalls = rawCalls
		}
	}
	if v, ok := obj["tool_call_id"].(string); ok {
		node.ToolCallID = v
	}
	if v, ok := obj["name"].(string); ok {
		node.Name = v
	}
}

func contentText(v any) string {
	switch c := v.(type) {
	case nil:
		return ""
	case string:
		return c
	case []any:
		var parts []string
		for _, item := range c {
			switch part := item.(type) {
			case string:
				parts = append(parts, part)
			case map[string]any:
				if text, ok := part["text"].(string); ok {
					parts = append(parts, text)
				} else if raw, err := json.Marshal(part); err == nil {
					parts = append(parts, string(raw))
				}
			default:
				if raw, err := json.Marshal(part); err == nil {
					parts = append(parts, string(raw))
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		raw, err := json.Marshal(c)
		if err != nil {
			return fmt.Sprint(c)
		}
		return string(raw)
	}
}

func reasoningDetailsText(v any) string {
	items, ok := v.([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, item := range items {
		detail, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := detail["text"].(string); ok && text != "" {
			parts = append(parts, text)
		}
		if summary, ok := detail["summary"].(string); ok && summary != "" {
			parts = append(parts, summary)
		}
	}
	return strings.Join(parts, "\n")
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
