// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package openai

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestStreamReconstructorComplete(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"cmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}}]}}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]}}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"reasoning_content":"think"},"finish_reason":null}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"prompt_cache_hit_tokens":2,"prompt_cache_miss_tokens":8,"cost":0.001}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	r := NewStreamReconstructor()
	// Feed byte-by-byte to exercise partial SSE lines.
	for i := 0; i < len(stream); i++ {
		r.Feed([]byte{stream[i]})
	}
	raw, complete, err := r.Finish()
	if err != nil {
		t.Fatalf("Finish() error: %v", err)
	}
	if !complete {
		t.Fatal("expected complete stream")
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("invalid reconstructed JSON: %v", err)
	}
	if out["id"] != "cmpl-1" || out["model"] != "gpt-test" {
		t.Fatalf("unexpected id/model: %#v", out)
	}
	choices := out["choices"].([]any)
	choice := choices[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("unexpected finish_reason: %#v", choice["finish_reason"])
	}
	msg := choice["message"].(map[string]any)
	if msg["content"] != "Hello" {
		t.Fatalf("unexpected content: %#v", msg["content"])
	}
	if msg["reasoning_content"] != "think" {
		t.Fatalf("unexpected reasoning: %#v", msg["reasoning_content"])
	}
	calls := msg["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(calls))
	}
	call := calls[0].(map[string]any)
	if call["id"] != "call_1" {
		t.Fatalf("unexpected tool call id: %#v", call)
	}
	fn := call["function"].(map[string]any)
	if fn["name"] != "lookup" || fn["arguments"] != `{"q":"x"}` {
		t.Fatalf("unexpected function: %#v", fn)
	}
	usage := out["usage"].(map[string]any)
	if usage["prompt_cache_hit_tokens"].(float64) != 2 {
		t.Fatalf("unexpected usage: %#v", usage)
	}
}

func TestStreamReconstructorIncomplete(t *testing.T) {
	stream := `data: {"id":"cmpl-2","choices":[{"index":0,"delta":{"role":"assistant","content":"partial"},"finish_reason":null}]}` + "\n\n"
	r := NewStreamReconstructor()
	r.Feed([]byte(stream))
	raw, complete, err := r.Finish()
	if err != nil {
		t.Fatalf("Finish() error: %v", err)
	}
	if complete {
		t.Fatal("expected incomplete stream")
	}
	if !strings.Contains(string(raw), `"partial"`) {
		t.Fatalf("expected partial content, got %s", raw)
	}
}

func TestStreamReconstructorFinishReasonWithoutDoneIsIncomplete(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"cmpl-3","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
	}, "\n")
	r := NewStreamReconstructor()
	r.Feed([]byte(stream))
	raw, complete, err := r.Finish()
	if err != nil {
		t.Fatalf("Finish() error: %v", err)
	}
	if complete {
		t.Fatal("expected incomplete stream without [DONE]")
	}
	if !strings.Contains(string(raw), `"hi"`) {
		t.Fatalf("expected partial reconstruction, got %s", raw)
	}
}

func TestStreamReconstructorErrorObjectIsComplete(t *testing.T) {
	stream := `data: {"error":{"message":"rate limited","type":"rate_limit"}}` + "\n\n"
	r := NewStreamReconstructor()
	r.Feed([]byte(stream))
	raw, complete, err := r.Finish()
	if err != nil {
		t.Fatalf("Finish() error: %v", err)
	}
	if !complete {
		t.Fatal("expected terminal error object to complete the stream")
	}
	if !strings.Contains(string(raw), "rate limited") {
		t.Fatalf("expected error in reconstruction, got %s", raw)
	}
}

func TestStreamReconstructorMalformed(t *testing.T) {
	r := NewStreamReconstructor()
	r.Feed([]byte("data: not-json\n\n"))
	raw, complete, err := r.Finish()
	if err == nil {
		t.Fatal("expected parse error")
	}
	if complete || raw != nil {
		t.Fatalf("expected no reconstruction, complete=%v raw=%s", complete, raw)
	}
}

func TestStreamReconstructorReasoningDetails(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"r1","choices":[{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.text","index":0,"text":"think "}]},"finish_reason":null}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.text","index":0,"text":"more"}]},"finish_reason":null}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	r := NewStreamReconstructor()
	r.Feed([]byte(stream))
	raw, complete, err := r.Finish()
	if err != nil || !complete {
		t.Fatalf("Finish() = %v, %v", complete, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	choice := out["choices"].([]any)[0].(map[string]any)
	msg := choice["message"].(map[string]any)
	details := msg["reasoning_details"].([]any)
	detail := details[0].(map[string]any)
	if detail["type"] != "reasoning.text" || detail["text"] != "think more" {
		t.Fatalf("unexpected reasoning details: %#v", detail)
	}
}

func TestStreamReconstructorNoFinalNewline(t *testing.T) {
	stream := `data: {"choices":[{"index":0,"delta":{"content":"x"}}]}` + "\n\n" + `data: [DONE]`
	r := NewStreamReconstructor()
	r.Feed([]byte(stream))
	raw, complete, err := r.Finish()
	if err != nil {
		t.Fatalf("Finish() error: %v", err)
	}
	if !complete {
		t.Fatal("expected complete stream")
	}
	if !strings.Contains(string(raw), `"x"`) {
		t.Fatalf("missing content: %s", raw)
	}
}

func TestStreamReconstructorEOFMidEvent(t *testing.T) {
	stream := `data: {"choices":[{"index":0,"delta":{"content":"x"}}]}`
	r := NewStreamReconstructor()
	r.Feed([]byte(stream))
	raw, complete, err := r.Finish()
	if err != nil {
		t.Fatalf("Finish() error: %v", err)
	}
	if complete {
		t.Fatal("expected incomplete stream without [DONE]")
	}
	if !strings.Contains(string(raw), `"x"`) {
		t.Fatalf("missing content: %s", raw)
	}
}

func TestStreamReconstructorMultipleDataLines(t *testing.T) {
	stream := "data: {\"choices\":[\ndata: {\"index\":0,\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n\n"
	r := NewStreamReconstructor()
	r.Feed([]byte(stream))
	raw, complete, err := r.Finish()
	if err != nil || !complete {
		t.Fatalf("Finish() = %v, %v", complete, err)
	}
	if !strings.Contains(string(raw), `"x"`) {
		t.Fatalf("missing content: %s", raw)
	}
}

func TestStreamReconstructorCommentsAndCRLF(t *testing.T) {
	stream := ": comment\r\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}]}\r\n\r\n: another\r\ndata: [DONE]\r\n\r\n"
	r := NewStreamReconstructor()
	r.Feed([]byte(stream))
	raw, complete, err := r.Finish()
	if err != nil || !complete {
		t.Fatalf("Finish() = %v, %v", complete, err)
	}
	if !strings.Contains(string(raw), `"x"`) {
		t.Fatalf("missing content: %s", raw)
	}
}

func TestStreamReconstructorEventField(t *testing.T) {
	stream := "event: message\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}]}\n\nevent: done\ndata: [DONE]\n\n"
	r := NewStreamReconstructor()
	r.Feed([]byte(stream))
	raw, complete, err := r.Finish()
	if err != nil || !complete {
		t.Fatalf("Finish() = %v, %v", complete, err)
	}
	if !strings.Contains(string(raw), `"x"`) {
		t.Fatalf("missing content: %s", raw)
	}
}

func TestStreamReconstructorMalformedThenValid(t *testing.T) {
	stream := "data: not-json\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n\n"
	r := NewStreamReconstructor()
	r.Feed([]byte(stream))
	raw, complete, err := r.Finish()
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !complete {
		t.Fatal("expected [DONE] to complete the stream")
	}
	if !strings.Contains(string(raw), `"x"`) {
		t.Fatalf("valid event after malformed one was not processed: %s", raw)
	}
}

func TestStreamReconstructorDoneWithWhitespace(t *testing.T) {
	stream := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}]}\n\ndata:   [DONE]   \n\n"
	r := NewStreamReconstructor()
	r.Feed([]byte(stream))
	_, complete, err := r.Finish()
	if err != nil || !complete {
		t.Fatalf("Finish() = %v, %v", complete, err)
	}
}

func TestStreamReconstructorRejectsBadToolCallIndex(t *testing.T) {
	cases := []struct{ name, index string }{
		{"negative", "-1"},
		{"fractional", "1.5"},
		{"string", `"0"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":` + tc.index + `,"id":"call_1","type":"function","function":{"name":"f","arguments":"{}"}}]}}]}` + "\n\ndata: [DONE]\n\n"
			r := NewStreamReconstructor()
			r.Feed([]byte(stream))
			raw, complete, err := r.Finish()
			if err == nil {
				t.Fatal("expected index error")
			}
			if !complete {
				t.Fatal("expected [DONE] to complete the stream")
			}
			if !strings.Contains(string(raw), `"call_1"`) {
				t.Fatalf("tool call data lost: %s", raw)
			}
		})
	}
}

func TestStreamReconstructorRejectsDuplicateToolCallIndex(t *testing.T) {
	stream := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"f","arguments":"{}"}},{"index":0,"id":"b","type":"function","function":{"name":"g","arguments":"{}"}}]}}]}` + "\n\ndata: [DONE]\n\n"
	r := NewStreamReconstructor()
	r.Feed([]byte(stream))
	_, complete, err := r.Finish()
	if err == nil {
		t.Fatal("expected duplicate index error")
	}
	if !complete {
		t.Fatal("expected [DONE] to complete the stream")
	}
}

func TestStreamReconstructorEventSizeLimit(t *testing.T) {
	r := NewStreamReconstructor()
	r.Feed(bytes.Repeat([]byte("a"), maxEventBytes+1))
	raw, complete, err := r.Finish()
	if err == nil {
		t.Fatal("expected event size error")
	}
	if complete || raw != nil {
		t.Fatalf("expected no reconstruction, complete=%v raw=%s", complete, raw)
	}
}

func TestStreamReconstructorErrorObjectWithParseError(t *testing.T) {
	stream := "data: not-json\n\ndata: {\"error\":{\"message\":\"boom\"}}\n\n"
	r := NewStreamReconstructor()
	r.Feed([]byte(stream))
	raw, complete, err := r.Finish()
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !complete {
		t.Fatal("terminal error object should complete the stream")
	}
	if !strings.Contains(string(raw), "boom") {
		t.Fatalf("missing error object: %s", raw)
	}
}
