// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// StreamReconstructor incrementally parses an OpenAI Chat Completions SSE
// stream and reconstructs a complete chat.completion response object.
type StreamReconstructor struct {
	sse sseParser

	seenDone bool

	id                string
	object            string
	created           any
	model             string
	systemFingerprint string
	serviceTier       string
	usage             map[string]any
	errObj            any

	choices  map[int]*choiceAcc
	parseErr error
}

type choiceAcc struct {
	index int

	role string

	content     strings.Builder
	contentSeen bool

	reasoning            strings.Builder
	reasoningSeen        bool
	reasoningContent     strings.Builder
	reasoningContentSeen bool

	refusal     strings.Builder
	refusalSeen bool

	toolCalls     map[int]*toolCallAcc
	toolCallOrder []int
	functionCall  *functionCallAcc

	reasoningDetails     map[int]*reasoningDetailAcc
	reasoningDetailOrder []int

	finishReason string
}

type reasoningDetailAcc struct {
	index   int
	values  map[string]any
	seen    map[string]bool
	text    strings.Builder
	summary strings.Builder
	data    strings.Builder
	args    strings.Builder
	result  strings.Builder
}

type toolCallAcc struct {
	id        string
	typ       string
	name      string
	arguments strings.Builder
}

type functionCallAcc struct {
	name      string
	arguments strings.Builder
}

// maxEventBytes caps a single SSE event. Events are small in normal LLM
// streams; a larger event is treated as malformed instead of growing memory
// without bound.
const maxEventBytes = 8 << 20

type sseParser struct {
	buf       []byte
	data      []string
	dataBytes int
	event     string
	err       error
}

type sseEvent struct {
	Event string
	Data  string
}

func NewStreamReconstructor() *StreamReconstructor {
	return &StreamReconstructor{
		choices: make(map[int]*choiceAcc),
	}
}

// Feed consumes a chunk of the SSE byte stream. It never blocks or emits
// events; only the reconstructed response is retained.
func (s *StreamReconstructor) Feed(p []byte) {
	for _, ev := range s.sse.feed(p) {
		s.handleEvent(ev)
	}
	s.recordSSEError()
}

func (s *StreamReconstructor) recordSSEError() {
	if s.sse.err != nil {
		s.noteError("%v", s.sse.err)
	}
}

func (s *StreamReconstructor) noteError(format string, args ...any) {
	if s.parseErr == nil {
		s.parseErr = fmt.Errorf(format, args...)
	}
}

// Finish returns the reconstructed response, whether the stream appeared to
// complete normally, and a parse error if the stream was malformed.
func (s *StreamReconstructor) Finish() (json.RawMessage, bool, error) {
	for _, ev := range s.sse.finish() {
		s.handleEvent(ev)
	}
	s.recordSSEError()
	raw, err := s.build()
	if err != nil {
		return nil, false, err
	}
	return raw, s.Complete(), s.parseErr
}

// Complete reports whether the stream ended with an explicit terminator. A
// [DONE] marker is required for a normally finished stream; a terminal error
// object also ends the stream. A finish_reason alone is not sufficient,
// because the connection may have been cut before the final frames.
func (s *StreamReconstructor) Complete() bool {
	return s.seenDone || s.errObj != nil
}

func (s *StreamReconstructor) handleEvent(ev sseEvent) {
	data := strings.TrimSpace(ev.Data)
	if data == "" {
		return
	}
	if data == "[DONE]" {
		s.seenDone = true
		return
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		if s.parseErr == nil {
			s.parseErr = fmt.Errorf("invalid stream JSON: %w", err)
		}
		return
	}
	s.handleChunk(chunk)
}

func (s *StreamReconstructor) handleChunk(chunk map[string]any) {
	if v, ok := chunk["id"].(string); ok && v != "" {
		s.id = v
	}
	if v, ok := chunk["object"].(string); ok && v != "" {
		s.object = v
	}
	if v, ok := chunk["created"]; ok && v != nil {
		s.created = v
	}
	if v, ok := chunk["model"].(string); ok && v != "" {
		s.model = v
	}
	if v, ok := chunk["system_fingerprint"].(string); ok && v != "" {
		s.systemFingerprint = v
	}
	if v, ok := chunk["service_tier"].(string); ok && v != "" {
		s.serviceTier = v
	}
	if v, ok := chunk["usage"].(map[string]any); ok {
		s.usage = v
	}
	if v, ok := chunk["error"]; ok && v != nil {
		s.errObj = v
	}

	choices, ok := chunk["choices"].([]any)
	if !ok {
		return
	}
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]any)
		if !ok {
			continue
		}
		idx := 0
		if v, ok := choice["index"]; ok {
			idx, _ = s.streamIndex(v, 0)
		}
		acc := s.choices[idx]
		if acc == nil {
			acc = &choiceAcc{
				index:            idx,
				toolCalls:        make(map[int]*toolCallAcc),
				reasoningDetails: make(map[int]*reasoningDetailAcc),
			}
			s.choices[idx] = acc
		}
		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			acc.finishReason = fr
		}
		delta, ok := choice["delta"].(map[string]any)
		if !ok {
			continue
		}
		s.handleDelta(acc, delta)
	}
}

func (s *StreamReconstructor) handleDelta(acc *choiceAcc, delta map[string]any) {
	if v, ok := delta["role"].(string); ok && v != "" {
		acc.role = v
	}
	if v, ok := delta["content"]; ok && v != nil {
		acc.contentSeen = true
		appendStringValue(&acc.content, v)
	}
	if v, ok := delta["reasoning_content"]; ok && v != nil {
		acc.reasoningContentSeen = true
		appendStringValue(&acc.reasoningContent, v)
	}
	if v, ok := delta["reasoning"]; ok && v != nil {
		acc.reasoningSeen = true
		appendStringValue(&acc.reasoning, v)
	}
	if v, ok := delta["refusal"]; ok && v != nil {
		acc.refusalSeen = true
		appendStringValue(&acc.refusal, v)
	}

	if rawDetails, ok := delta["reasoning_details"].([]any); ok {
		seen := make(map[int]bool, len(rawDetails))
		for pos, rawDetail := range rawDetails {
			detail, ok := rawDetail.(map[string]any)
			if !ok {
				continue
			}
			idx := pos
			if v, ok := detail["index"]; ok {
				idx, _ = s.streamIndex(v, pos)
			}
			if seen[idx] {
				s.noteError("duplicate reasoning detail index %d", idx)
			}
			seen[idx] = true
			d := acc.reasoningDetails[idx]
			if d == nil {
				d = &reasoningDetailAcc{
					index:  idx,
					values: make(map[string]any),
					seen:   make(map[string]bool),
				}
				acc.reasoningDetails[idx] = d
				acc.reasoningDetailOrder = append(acc.reasoningDetailOrder, idx)
			}
			d.merge(detail)
		}
	}

	if rawCalls, ok := delta["tool_calls"].([]any); ok {
		seen := make(map[int]bool, len(rawCalls))
		for pos, rawCall := range rawCalls {
			call, ok := rawCall.(map[string]any)
			if !ok {
				continue
			}
			idx := pos
			if v, ok := call["index"]; ok {
				idx, _ = s.streamIndex(v, pos)
			}
			if seen[idx] {
				s.noteError("duplicate tool call index %d", idx)
			}
			seen[idx] = true
			tc := acc.toolCalls[idx]
			if tc == nil {
				tc = &toolCallAcc{}
				acc.toolCalls[idx] = tc
				acc.toolCallOrder = append(acc.toolCallOrder, idx)
			}
			if v, ok := call["id"].(string); ok && v != "" {
				tc.id = v
			}
			if v, ok := call["type"].(string); ok && v != "" {
				tc.typ = v
			}
			if fn, ok := call["function"].(map[string]any); ok {
				if v, ok := fn["name"].(string); ok && v != "" {
					tc.name = v
				}
				if v, ok := fn["arguments"]; ok && v != nil {
					appendStringValue(&tc.arguments, v)
				}
			}
		}
	}

	if fn, ok := delta["function_call"].(map[string]any); ok {
		if acc.functionCall == nil {
			acc.functionCall = &functionCallAcc{}
		}
		if v, ok := fn["name"].(string); ok && v != "" {
			acc.functionCall.name = v
		}
		if v, ok := fn["arguments"]; ok && v != nil {
			appendStringValue(&acc.functionCall.arguments, v)
		}
	}
}

func (s *StreamReconstructor) build() (json.RawMessage, error) {
	if len(s.choices) == 0 && s.usage == nil && s.errObj == nil && s.parseErr != nil {
		return nil, s.parseErr
	}

	out := map[string]any{
		"object": "chat.completion",
	}
	if s.object != "" {
		out["object"] = s.object
	}
	if s.id != "" {
		out["id"] = s.id
	}
	if s.created != nil {
		out["created"] = s.created
	}
	if s.model != "" {
		out["model"] = s.model
	}
	if s.systemFingerprint != "" {
		out["system_fingerprint"] = s.systemFingerprint
	}
	if s.serviceTier != "" {
		out["service_tier"] = s.serviceTier
	}

	indexes := make([]int, 0, len(s.choices))
	for idx := range s.choices {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	choices := make([]any, 0, len(indexes))
	for _, idx := range indexes {
		acc := s.choices[idx]
		msg := map[string]any{}
		role := acc.role
		if role == "" {
			role = "assistant"
		}
		msg["role"] = role

		if acc.contentSeen {
			msg["content"] = acc.content.String()
		} else if len(acc.toolCalls) == 0 && acc.functionCall == nil {
			msg["content"] = ""
		} else {
			msg["content"] = nil
		}
		if acc.reasoningContentSeen {
			msg["reasoning_content"] = acc.reasoningContent.String()
		}
		if acc.reasoningSeen {
			msg["reasoning"] = acc.reasoning.String()
		}
		if len(acc.reasoningDetails) > 0 {
			order := append([]int(nil), acc.reasoningDetailOrder...)
			sort.Ints(order)
			details := make([]any, 0, len(order))
			for _, detailIdx := range order {
				details = append(details, acc.reasoningDetails[detailIdx].build())
			}
			msg["reasoning_details"] = details
		}
		if acc.refusalSeen {
			msg["refusal"] = acc.refusal.String()
		}

		if len(acc.toolCalls) > 0 {
			order := append([]int(nil), acc.toolCallOrder...)
			sort.Ints(order)
			calls := make([]any, 0, len(order))
			for _, callIdx := range order {
				tc := acc.toolCalls[callIdx]
				fn := map[string]any{
					"name":      tc.name,
					"arguments": tc.arguments.String(),
				}
				call := map[string]any{
					"type":     tc.typ,
					"function": fn,
				}
				if call["type"] == "" {
					call["type"] = "function"
				}
				if tc.id != "" {
					call["id"] = tc.id
				}
				calls = append(calls, call)
			}
			msg["tool_calls"] = calls
		}
		if acc.functionCall != nil {
			msg["function_call"] = map[string]any{
				"name":      acc.functionCall.name,
				"arguments": acc.functionCall.arguments.String(),
			}
		}

		var finish any
		if acc.finishReason != "" {
			finish = acc.finishReason
		}
		choices = append(choices, map[string]any{
			"index":         idx,
			"message":       msg,
			"finish_reason": finish,
		})
	}
	out["choices"] = choices
	if s.usage != nil {
		out["usage"] = s.usage
	}
	if s.errObj != nil {
		out["error"] = s.errObj
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (d *reasoningDetailAcc) merge(item map[string]any) {
	for k, v := range item {
		switch k {
		case "index":
			continue
		case "text", "summary", "data", "arguments", "result":
			if str, ok := v.(string); ok {
				d.seen[k] = true
				switch k {
				case "text":
					d.text.WriteString(str)
				case "summary":
					d.summary.WriteString(str)
				case "data":
					d.data.WriteString(str)
				case "arguments":
					d.args.WriteString(str)
				case "result":
					d.result.WriteString(str)
				}
			} else if v != nil {
				d.values[k] = v
			}
		case "type", "id", "format", "signature", "tool_call_id", "tool_name":
			if str, ok := v.(string); ok && str != "" {
				d.values[k] = str
			} else if v != nil && v != "" {
				d.values[k] = v
			}
		default:
			if v != nil {
				d.values[k] = v
			}
		}
	}
}

func (d *reasoningDetailAcc) build() map[string]any {
	out := map[string]any{"index": d.index}
	for k, v := range d.values {
		out[k] = v
	}
	if d.seen["text"] {
		out["text"] = d.text.String()
	}
	if d.seen["summary"] {
		out["summary"] = d.summary.String()
	}
	if d.seen["data"] {
		out["data"] = d.data.String()
	}
	if d.seen["arguments"] {
		out["arguments"] = d.args.String()
	}
	if d.seen["result"] {
		out["result"] = d.result.String()
	}
	return out
}

func appendStringValue(b *strings.Builder, v any) {
	switch x := v.(type) {
	case string:
		b.WriteString(x)
	default:
		raw, err := json.Marshal(x)
		if err == nil {
			b.Write(raw)
		}
	}
}

func (s *StreamReconstructor) streamIndex(value any, fallback int) (int, bool) {
	idx, ok := nonNegativeInt(value)
	if !ok {
		s.noteError("invalid stream index %v", value)
		return fallback, false
	}
	return idx, true
}

func nonNegativeInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		i := int(n)
		if float64(i) == n && i >= 0 {
			return i, true
		}
	case int:
		if n >= 0 {
			return n, true
		}
	case int64:
		if n >= 0 {
			return int(n), true
		}
	case json.Number:
		if i, err := n.Int64(); err == nil && i >= 0 {
			return int(i), true
		}
	}
	return 0, false
}

func (p *sseParser) feed(data []byte) []sseEvent {
	if p.err != nil {
		return nil
	}
	p.buf = append(p.buf, data...)
	var out []sseEvent
	for {
		idx := bytes.IndexByte(p.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(p.buf[:idx])
		p.buf = p.buf[idx+1:]
		line = strings.TrimSuffix(line, "\r")
		if ev, ok := p.processLine(line); ok {
			out = append(out, ev)
		}
		if p.dataBytes > maxEventBytes {
			p.err = fmt.Errorf("SSE event exceeds %d bytes", maxEventBytes)
			p.data = nil
			p.dataBytes = 0
			return out
		}
	}
	if len(p.buf) > maxEventBytes {
		p.err = fmt.Errorf("SSE line exceeds %d bytes", maxEventBytes)
		p.buf = nil
	}
	return out
}

func (p *sseParser) finish() []sseEvent {
	if p.err != nil {
		return nil
	}
	var out []sseEvent
	if len(p.buf) > 0 {
		line := string(p.buf)
		p.buf = nil
		line = strings.TrimSuffix(line, "\r")
		if ev, ok := p.processLine(line); ok {
			out = append(out, ev)
		}
	}
	if p.dataBytes > maxEventBytes {
		p.err = fmt.Errorf("SSE event exceeds %d bytes", maxEventBytes)
		p.data = nil
		p.dataBytes = 0
		return nil
	}
	if ev, ok := p.dispatch(); ok {
		out = append(out, ev)
	}
	return out
}

func (p *sseParser) processLine(line string) (sseEvent, bool) {
	if line == "" {
		return p.dispatch()
	}
	if strings.HasPrefix(line, ":") {
		return sseEvent{}, false
	}
	field, value, found := strings.Cut(line, ":")
	if !found {
		field = line
		value = ""
	} else if strings.HasPrefix(value, " ") {
		value = value[1:]
	}
	switch field {
	case "event":
		p.event = value
	case "data":
		p.data = append(p.data, value)
		p.dataBytes += len(value) + 1
	}
	return sseEvent{}, false
}

func (p *sseParser) dispatch() (sseEvent, bool) {
	if len(p.data) == 0 && p.event == "" {
		return sseEvent{}, false
	}
	ev := sseEvent{Event: p.event, Data: strings.Join(p.data, "\n")}
	p.data = nil
	p.dataBytes = 0
	p.event = ""
	return ev, true
}
