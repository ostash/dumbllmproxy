// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package proxy_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"dumbllmproxy/internal/config"
	"dumbllmproxy/internal/cost"
	"dumbllmproxy/internal/pricing"
	"dumbllmproxy/internal/proxy"
	"dumbllmproxy/internal/store"

	"github.com/klauspost/compress/zstd"
)

func newTestProxy(t *testing.T, baseURL, parser, pricingName string) (*store.Store, *httptest.Server) {
	t.Helper()
	cfg := &config.Config{
		Proxy: config.ProxyConfig{Listen: "127.0.0.1:0", Database: filepath.Join(t.TempDir(), "test.db")},
		Upstreams: []config.UpstreamConfig{{
			Name:    "test-upstream",
			Prefix:  "/open",
			BaseURL: baseURL,
			Parser:  parser,
			Pricing: pricingName,
		}},
		SessionHeaders:  []string{"x-session-id", "x-conversation-id"},
		MetadataHeaders: []string{"x-harness-id"},
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("invalid test config: %v", err)
	}
	st, err := store.Open(cfg.Proxy.Database)
	if err != nil {
		t.Fatal(err)
	}
	st.SetCostResolver(cost.NewResolver(pricing.NewRegistry()).Resolve)
	h := proxy.New(cfg, st, log.New(io.Discard, "", 0))
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		srv.Close()
		st.Close()
	})
	return st, srv
}

func waitForRequests(t *testing.T, st *store.Store, sessionID int64) []store.RequestSummary {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		reqs, err := st.ListRequests(context.Background(), sessionID)
		if err == nil && len(reqs) > 0 {
			return reqs
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for captured request")
	return nil
}

func TestProxyNonStreamingAndOpenRouterCost(t *testing.T) {
	const upstreamBody = `{"id":"chatcmpl-1","object":"chat.completion","model":"resp-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15,"prompt_tokens_details":{"cached_tokens":2,"cache_write_tokens":1},"completion_tokens_details":{"reasoning_tokens":1},"cost":0.0042}}`
	var (
		gotPath   string
		gotQuery  string
		gotBody   []byte
		gotHeader http.Header
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotBody, _ = io.ReadAll(r.Body)
		gotHeader = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream", "yes")
		io.WriteString(w, upstreamBody)
	}))
	defer upstream.Close()

	st, proxySrv := newTestProxy(t, upstream.URL+"/base", "openai_chat", "openrouter")
	reqBody := `{"model":"req-model","messages":[{"role":"user","content":"hi"}],"stream":false}`
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/v1/chat/completions?x=1", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-Id", "sess-1")
	req.Header.Set("X-Conversation-Id", "conversation-2")
	req.Header.Set("X-Harness-Id", "harness-9")
	req.Header.Set("Authorization", "Bearer super-secret")
	req.Header.Set("Cookie", "session=secret-cookie")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(respBody) != upstreamBody {
		t.Fatalf("response body was changed: %q", respBody)
	}
	if resp.Header.Get("X-Upstream") != "yes" {
		t.Fatalf("upstream response header not forwarded")
	}
	if gotPath != "/base/v1/chat/completions" || gotQuery != "x=1" {
		t.Fatalf("unexpected upstream URL: path=%q query=%q", gotPath, gotQuery)
	}
	if !bytes.Equal(gotBody, []byte(reqBody)) {
		t.Fatalf("request body changed: %s", gotBody)
	}
	if gotHeader.Get("Authorization") != "Bearer super-secret" {
		t.Fatalf("authorization header was not passed through")
	}

	sessions, err := st.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].SessionKey != "sess-1" {
		t.Fatalf("unexpected sessions: %#v", sessions)
	}
	if sessions[0].CachedInputTokens == nil || *sessions[0].CachedInputTokens != 2 {
		t.Fatalf("session cached tokens not aggregated: %#v", sessions[0].CachedInputTokens)
	}
	if sessions[0].ReasoningTokens == nil || *sessions[0].ReasoningTokens != 1 {
		t.Fatalf("session reasoning tokens not aggregated: %#v", sessions[0].ReasoningTokens)
	}
	reqs := waitForRequests(t, st, sessions[0].ID)
	if len(reqs) != 1 {
		t.Fatalf("expected one request, got %d", len(reqs))
	}
	r := reqs[0]
	if r.Provider != "test-upstream" || r.Model != "req-model" || r.StatusCode != 200 {
		t.Fatalf("unexpected request summary: %#v", r)
	}
	if r.Harness != "harness-9" {
		t.Fatalf("unexpected harness: %q", r.Harness)
	}
	if r.CostAmount == nil || *r.CostAmount != 0.0042 || r.CostSource != "openrouter_reported" {
		t.Fatalf("unexpected cost: %#v", r)
	}
	if r.SessionHeader != "X-Session-Id" {
		t.Fatalf("unexpected session header: %q", r.SessionHeader)
	}
	if r.CachedInputTokens == nil || *r.CachedInputTokens != 2 {
		t.Fatalf("request cached tokens not extracted: %#v", r.CachedInputTokens)
	}
	if r.ReasoningTokens == nil || *r.ReasoningTokens != 1 {
		t.Fatalf("request reasoning tokens not extracted: %#v", r.ReasoningTokens)
	}

	detail, err := st.GetRequest(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.RequestBody != reqBody {
		t.Fatalf("stored request body changed: %q", detail.RequestBody)
	}
	if detail.ResponseBody != upstreamBody {
		t.Fatalf("stored response body changed: %q", detail.ResponseBody)
	}
	if !strings.Contains(detail.MetadataJSON, "harness-9") {
		t.Fatalf("metadata header missing: %s", detail.MetadataJSON)
	}
	if strings.Contains(detail.MetadataJSON, "super-secret") || strings.Contains(detail.MetadataJSON, "secret-cookie") {
		t.Fatalf("sensitive header leaked into metadata: %s", detail.MetadataJSON)
	}
	if detail.CachedInputTokens == nil || *detail.CachedInputTokens != 2 {
		t.Fatalf("cached tokens not extracted: %#v", detail.CachedInputTokens)
	}
	if detail.CacheWriteTokens == nil || *detail.CacheWriteTokens != 1 {
		t.Fatalf("cache write tokens not extracted: %#v", detail.CacheWriteTokens)
	}
	if detail.ReasoningTokens == nil || *detail.ReasoningTokens != 1 {
		t.Fatalf("reasoning tokens not extracted: %#v", detail.ReasoningTokens)
	}
	if detail.Harness != "harness-9" {
		t.Fatalf("unexpected detail harness: %q", detail.Harness)
	}
}

func TestProxyStreamingPassthroughAndReconstruction(t *testing.T) {
	release := make(chan struct{})
	firstChunk := "data: {\"id\":\"cmpl-s\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"stream-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hel\"},\"finish_reason\":null}]}\n\n"
	finalChunks := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9,\"cost\":0.0007}}\n\n" +
		"data: [DONE]\n\n"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)
		io.WriteString(w, firstChunk)
		flusher.Flush()
		<-release
		io.WriteString(w, finalChunks)
		flusher.Flush()
	}))
	defer upstream.Close()

	st, proxySrv := newTestProxy(t, upstream.URL, "openai_chat", "openrouter")
	reqBody := `{"model":"stream-model","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-Id", "stream-session")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	lineCh := make(chan string, 1)
	go func() {
		line, _ := reader.ReadString('\n')
		lineCh <- line
	}()
	select {
	case line := <-lineCh:
		if !strings.Contains(line, "Hel") {
			t.Fatalf("unexpected first forwarded line: %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first stream chunk was not forwarded in real time")
	}
	close(release)
	rest, _ := io.ReadAll(reader)
	all := firstChunk + string(rest)
	if !strings.Contains(all, "Hel") || !strings.Contains(all, `"content":"lo"`) || !strings.Contains(all, "[DONE]") {
		t.Fatalf("stream incomplete at client: %q", all)
	}

	reqs := waitForRequests(t, st, waitForSessionID(t, st))
	if len(reqs) != 1 {
		t.Fatalf("expected one request, got %d", len(reqs))
	}
	r := reqs[0]
	if !r.Streamed || r.Incomplete {
		t.Fatalf("unexpected stream flags: streamed=%v incomplete=%v", r.Streamed, r.Incomplete)
	}
	detail, err := st.GetRequest(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.RawResponseBody != "" {
		t.Fatalf("complete stream should not store raw SSE fallback")
	}
	var reconstructed map[string]any
	if err := json.Unmarshal([]byte(detail.ResponseBody), &reconstructed); err != nil {
		t.Fatalf("stored response is not reconstructed JSON: %v\n%s", err, detail.ResponseBody)
	}
	choices := reconstructed["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "Hello" {
		t.Fatalf("unexpected reconstructed content: %#v", msg["content"])
	}
	if _, ok := msg["tool_calls"]; !ok {
		t.Fatalf("tool calls were not reconstructed: %#v", msg)
	}
	usage := reconstructed["usage"].(map[string]any)
	if usage["prompt_tokens"].(float64) != 7 {
		t.Fatalf("usage was not reconstructed: %#v", usage)
	}
	if r.TotalTokens == nil || *r.TotalTokens != 9 {
		t.Fatalf("usage not stored: %#v", r)
	}
	if r.CostAmount == nil || *r.CostAmount != 0.0007 || r.CostSource != "openrouter_reported" {
		t.Fatalf("streaming OpenRouter cost not stored: %#v", r)
	}
}

func waitForSessionID(t *testing.T, st *store.Store) int64 {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sessions, err := st.ListSessions(context.Background())
		if err == nil && len(sessions) > 0 {
			return sessions[0].ID
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for captured session")
	return 0
}

func TestProxyUnknownStreamStoresRawFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: custom\ndata: hello\n\n")
	}))
	defer upstream.Close()

	st, proxySrv := newTestProxy(t, upstream.URL, "none", "none")
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/custom",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("X-Session-Id", "raw-session")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	reqs := waitForRequests(t, st, waitForSessionID(t, st))
	detail, err := st.GetRequest(context.Background(), reqs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Streamed {
		t.Fatal("expected streamed=true")
	}
	if detail.ResponseBody != "event: custom\ndata: hello\n\n" {
		t.Fatalf("unexpected raw fallback: %q", detail.ResponseBody)
	}
}

func TestProxyCompressedResponsePassThroughAndDecodedStorage(t *testing.T) {
	const jsonBody = `{"id":"c1","object":"chat.completion","model":"m","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	var compressed bytes.Buffer
	gw := gzip.NewWriter(&compressed)
	_, _ = gw.Write([]byte(jsonBody))
	_ = gw.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(compressed.Bytes())
	}))
	defer upstream.Close()

	st, proxySrv := newTestProxy(t, upstream.URL, "openai_chat", "none")
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/v1/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("X-Session-Id", "gzip-session")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("content encoding not preserved: %q", resp.Header.Get("Content-Encoding"))
	}
	if !bytes.Equal(raw, compressed.Bytes()) {
		t.Fatalf("compressed bytes were changed in transit")
	}

	reqs := waitForRequests(t, st, waitForSessionID(t, st))
	detail, err := st.GetRequest(context.Background(), reqs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ResponseBody != jsonBody {
		t.Fatalf("stored body was not decoded: %q", detail.ResponseBody)
	}
}

func TestProxyUnassignedRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"m","choices":[],"usage":{}}`)
	}))
	defer upstream.Close()

	st, proxySrv := newTestProxy(t, upstream.URL, "openai_chat", "none")
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/v1/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	unassigned := true
	deadline := time.Now().Add(3 * time.Second)
	var reqs []store.RequestSummary
	for time.Now().Before(deadline) {
		reqs, _, err = st.ListAllRequestsFiltered(context.Background(),
			store.ListOptions{Unassigned: &unassigned, Limit: 10})
		if err == nil && len(reqs) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected unassigned request, got %d", len(reqs))
	}
}

func TestProxyAuxiliaryRequestNeverJoinsSession(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"id":"model-a"}]}`)
	}))
	defer upstream.Close()

	st, proxySrv := newTestProxy(t, upstream.URL, "openai_chat", "none")
	req, _ := http.NewRequest(http.MethodGet, proxySrv.URL+"/open/api/v1/models", nil)
	req.Header.Set("X-Session-Id", "session-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	deadline := time.Now().Add(3 * time.Second)
	var reqs []store.RequestSummary
	for time.Now().Before(deadline) {
		reqs, err = st.ListAllRequests(context.Background())
		if err == nil && len(reqs) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected one captured request, got %d", len(reqs))
	}
	if reqs[0].Conversation {
		t.Fatal("model listing must not be classified as a conversation")
	}
	if reqs[0].SessionID != nil || reqs[0].SessionKey != "" {
		t.Fatalf("auxiliary request joined a session: id=%v key=%q", reqs[0].SessionID, reqs[0].SessionKey)
	}
	sessions, err := st.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("auxiliary request created a session: %#v", sessions)
	}
}

func TestProxyDeepSeekCalculatedCost(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"d1","object":"chat.completion","model":"deepseek-chat","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3000000,"completion_tokens":3000000,"total_tokens":6000000,"prompt_cache_hit_tokens":1000000,"prompt_cache_miss_tokens":2000000}}`)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Proxy: config.ProxyConfig{Listen: "127.0.0.1:0", Database: filepath.Join(t.TempDir(), "test.db")},
		Upstreams: []config.UpstreamConfig{{
			Name:    "deepseek",
			Prefix:  "/deepseek",
			BaseURL: upstream.URL,
			Parser:  "openai_chat",
			Pricing: "deepseek",
		}},
		SessionHeaders: []string{"x-session-id"},
		Pricing: config.PricingConfig{DeepSeek: &config.DeepSeekPricing{
			Unit:             "per_million_tokens",
			ScheduleTimezone: "UTC",
			HolidayTimezone:  "UTC",
			Models: map[string]config.DeepSeekModelPricing{
				"deepseek-chat": {
					Rates: []config.PricingRate{{
						Name:           "default",
						InputCacheHit:  1,
						InputCacheMiss: 2,
						Output:         3,
					}},
				},
			},
		}},
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	ds, err := pricing.NewDeepSeek(*cfg.Pricing.DeepSeek)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.Proxy.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	registry := pricing.NewRegistry()
	if err := registry.Register("deepseek", ds); err != nil {
		t.Fatal(err)
	}
	st.SetCostResolver(cost.NewResolver(registry).Resolve)
	h := proxy.New(cfg, st, log.New(io.Discard, "", 0))
	proxySrv := httptest.NewServer(h)
	defer proxySrv.Close()

	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/deepseek/chat/completions", strings.NewReader(`{"model":"deepseek-chat"}`))
	req.Header.Set("X-Session-Id", "ds-session")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	reqs := waitForRequests(t, st, waitForSessionID(t, st))
	detail, err := st.GetRequest(context.Background(), reqs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.CostAmount == nil || *detail.CostAmount != 14 {
		t.Fatalf("expected cost 14, got %#v", detail.CostAmount)
	}
	if detail.CostSource != "deepseek_calculated" {
		t.Fatalf("unexpected cost source: %q", detail.CostSource)
	}
	if !strings.Contains(detail.CostDetailsJSON, `"cache_hit_cost":1`) {
		t.Fatalf("cost explanation missing: %s", detail.CostDetailsJSON)
	}
	if detail.PricingSnapshotJSON == "" {
		t.Fatal("pricing snapshot missing")
	}
}

func TestProxySessionHeaderFallbackOrder(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"m","choices":[],"usage":{}}`)
	}))
	defer upstream.Close()

	st, proxySrv := newTestProxy(t, upstream.URL, "openai_chat", "none")
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/v1/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("X-Conversation-Id", "conv-fallback")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	sessions, err := st.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].SessionKey != "conv-fallback" {
		t.Fatalf("unexpected sessions: %#v", sessions)
	}
	reqs := waitForRequests(t, st, sessions[0].ID)
	if reqs[0].SessionHeader != "X-Conversation-Id" {
		t.Fatalf("unexpected session header: %q", reqs[0].SessionHeader)
	}
}

func TestProxyDropsConnectionNominatedHeadersBothDirections(t *testing.T) {
	var gotHeader http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Keep", "yes")
		w.Header().Set("X-Upstream-Hop", "secret")
		w.Header().Set("Connection", "X-Upstream-Hop")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	_, proxySrv := newTestProxy(t, upstream.URL, "none", "none")
	req, _ := http.NewRequest(http.MethodGet, proxySrv.URL+"/open/models", nil)
	req.Header.Set("X-Client-Keep", "yes")
	req.Header.Set("X-Client-Hop", "secret")
	req.Header.Set("Connection", "X-Client-Hop")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if got := gotHeader.Get("X-Client-Keep"); got != "yes" {
		t.Fatalf("end-to-end request header lost: %q", got)
	}
	if got := gotHeader.Values("X-Client-Hop"); len(got) != 0 {
		t.Fatalf("connection-nominated request header forwarded: %#v", got)
	}
	if got := gotHeader.Values("Connection"); len(got) != 0 {
		t.Fatalf("Connection header forwarded: %#v", got)
	}

	if got := resp.Header.Get("X-Upstream-Keep"); got != "yes" {
		t.Fatalf("end-to-end response header lost: %q", got)
	}
	if got := resp.Header.Values("X-Upstream-Hop"); len(got) != 0 {
		t.Fatalf("connection-nominated response header forwarded: %#v", got)
	}
	if got := resp.Header.Values("Connection"); len(got) != 0 {
		t.Fatalf("upstream Connection header forwarded: %#v", got)
	}
}

func TestProxyPersistsOnlyAllowlistedHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Unconfigured-Response", "leak-response")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	st, proxySrv := newTestProxy(t, upstream.URL, "none", "none")
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/v1/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("X-Session-Id", "sess-allowlist")
	req.Header.Set("X-Harness-Id", "harness-allowlist")
	req.Header.Set("X-Unconfigured", "leak-request")
	req.Header.Set("Authorization", "Bearer super-secret")
	req.Header.Set("Cookie", "session=secret-cookie")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	sessions, err := st.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].SessionKey != "sess-allowlist" {
		t.Fatalf("unexpected sessions: %#v", sessions)
	}
	reqs := waitForRequests(t, st, sessions[0].ID)
	if len(reqs) != 1 {
		t.Fatalf("expected one request, got %d", len(reqs))
	}
	if reqs[0].SessionHeader != "X-Session-Id" {
		t.Fatalf("unexpected session header: %q", reqs[0].SessionHeader)
	}
	detail, err := st.GetRequest(context.Background(), reqs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(detail.MetadataJSON), &metadata); err != nil {
		t.Fatalf("metadata is not JSON: %v (%s)", err, detail.MetadataJSON)
	}
	want := map[string]any{"request.X-Harness-Id": "harness-allowlist"}
	if !reflect.DeepEqual(metadata, want) {
		t.Fatalf("unexpected persisted metadata: %#v", metadata)
	}
	for _, leak := range []string{"leak-request", "leak-response", "super-secret", "secret-cookie"} {
		if strings.Contains(detail.MetadataJSON, leak) {
			t.Fatalf("non-allowlisted value %q persisted: %s", leak, detail.MetadataJSON)
		}
	}
}

func TestProxyZstdWireEncoding(t *testing.T) {
	const requestJSON = `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	const responseJSON = `{"id":"c1","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`

	compress := func(s string) []byte {
		var buf bytes.Buffer
		w, err := zstd.NewWriter(&buf)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	requestCompressed := compress(requestJSON)
	responseCompressed := compress(responseJSON)

	var gotRequestBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "zstd")
		_, _ = w.Write(responseCompressed)
	}))
	defer upstream.Close()

	st, proxySrv := newTestProxy(t, upstream.URL, "openai_chat", "none")
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/v1/chat/completions", bytes.NewReader(requestCompressed))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "zstd")
	req.Header.Set("X-Session-Id", "zstd-session")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if !bytes.Equal(gotRequestBody, requestCompressed) {
		t.Fatalf("compressed request bytes were changed in transit")
	}
	if resp.Header.Get("Content-Encoding") != "zstd" {
		t.Fatalf("response content encoding not preserved: %q", resp.Header.Get("Content-Encoding"))
	}
	if !bytes.Equal(raw, responseCompressed) {
		t.Fatalf("compressed response bytes were changed in transit")
	}

	reqs := waitForRequests(t, st, waitForSessionID(t, st))
	detail, err := st.GetRequest(context.Background(), reqs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.RequestBody != requestJSON {
		t.Fatalf("stored request body was not decoded: %q", detail.RequestBody)
	}
	if detail.ResponseBody != responseJSON {
		t.Fatalf("stored response body was not decoded: %q", detail.ResponseBody)
	}
}

func TestProxyMultipleContentEncodingHeaders(t *testing.T) {
	const requestJSON = `{"model":"m","messages":[{"role":"user","content":"hi"}]}`

	var gzipped bytes.Buffer
	gw := gzip.NewWriter(&gzipped)
	_, _ = gw.Write([]byte(requestJSON))
	_ = gw.Close()
	var nested bytes.Buffer
	zw, err := zstd.NewWriter(&nested)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = zw.Write(gzipped.Bytes())
	_ = zw.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"m","choices":[],"usage":{}}`)
	}))
	defer upstream.Close()

	st, proxySrv := newTestProxy(t, upstream.URL, "openai_chat", "none")
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/v1/chat/completions", bytes.NewReader(nested.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Add("Content-Encoding", "gzip")
	req.Header.Add("Content-Encoding", "zstd")
	req.Header.Set("X-Session-Id", "multi-encoding")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	reqs := waitForRequests(t, st, waitForSessionID(t, st))
	detail, err := st.GetRequest(context.Background(), reqs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.RequestBody != requestJSON {
		t.Fatalf("nested request body was not decoded: %q", detail.RequestBody)
	}
}

func TestProxyStoresEndpointRelativeToPrefix(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"m","choices":[],"usage":{}}`)
	}))
	defer upstream.Close()

	st, proxySrv := newTestProxy(t, upstream.URL, "openai_chat", "none")
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/v1/chat/completions?x=1",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("X-Session-Id", "endpoint-session")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	reqs := waitForRequests(t, st, waitForSessionID(t, st))
	detail, err := st.GetRequest(context.Background(), reqs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Endpoint != "/v1/chat/completions?x=1" {
		t.Fatalf("unexpected endpoint: %q", detail.Endpoint)
	}
}

func TestProxyChunkedRequestBody(t *testing.T) {
	const reqBody = `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	var (
		gotBody          []byte
		gotContentLength int64
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotContentLength = r.ContentLength
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"m","choices":[],"usage":{}}`)
	}))
	defer upstream.Close()

	_, proxySrv := newTestProxy(t, upstream.URL, "openai_chat", "none")
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte(reqBody))
		_ = pw.Close()
	}()
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/v1/chat/completions", pr)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-Id", "chunked")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(gotBody) != reqBody {
		t.Fatalf("upstream body mismatch: %q", gotBody)
	}
	if gotContentLength != int64(len(reqBody)) {
		t.Fatalf("expected re-framed content length %d, got %d", len(reqBody), gotContentLength)
	}
}

func TestProxyStripsExpectHeader(t *testing.T) {
	var gotExpect string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotExpect = r.Header.Get("Expect")
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"m","choices":[],"usage":{}}`)
	}))
	defer upstream.Close()

	_, proxySrv := newTestProxy(t, upstream.URL, "openai_chat", "none")
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/v1/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Expect", "100-continue")
	req.Header.Set("X-Session-Id", "expect")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if gotExpect != "" {
		t.Fatalf("Expect header was forwarded: %q", gotExpect)
	}
}

func TestProxyCompressedRequestPassThroughAndDecodedStorage(t *testing.T) {
	const reqJSON = `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	var compressed bytes.Buffer
	gw := gzip.NewWriter(&compressed)
	_, _ = gw.Write([]byte(reqJSON))
	_ = gw.Close()

	var (
		gotBody     []byte
		gotEncoding string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotEncoding = r.Header.Get("Content-Encoding")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"m","choices":[],"usage":{}}`)
	}))
	defer upstream.Close()

	st, proxySrv := newTestProxy(t, upstream.URL, "openai_chat", "none")
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/v1/chat/completions",
		bytes.NewReader(compressed.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("X-Session-Id", "gzip-request")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if !bytes.Equal(gotBody, compressed.Bytes()) {
		t.Fatalf("compressed request bytes were changed in transit")
	}
	if gotEncoding != "gzip" {
		t.Fatalf("content encoding not preserved: %q", gotEncoding)
	}

	reqs := waitForRequests(t, st, waitForSessionID(t, st))
	detail, err := st.GetRequest(context.Background(), reqs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.RequestBody != reqJSON {
		t.Fatalf("stored request body was not decoded: %q", detail.RequestBody)
	}
}

func TestProxyNestedResponseEncoding(t *testing.T) {
	const respJSON = `{"id":"c1","object":"chat.completion","model":"m","choices":[],"usage":{}}`
	var gzipped bytes.Buffer
	gw := gzip.NewWriter(&gzipped)
	_, _ = gw.Write([]byte(respJSON))
	_ = gw.Close()
	var nested bytes.Buffer
	zw, err := zstd.NewWriter(&nested)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = zw.Write(gzipped.Bytes())
	_ = zw.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip, zstd")
		_, _ = w.Write(nested.Bytes())
	}))
	defer upstream.Close()

	st, proxySrv := newTestProxy(t, upstream.URL, "openai_chat", "none")
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/v1/chat/completions",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-Id", "nested")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if !bytes.Equal(raw, nested.Bytes()) {
		t.Fatalf("compressed response bytes were changed in transit")
	}
	if resp.Header.Get("Content-Encoding") != "gzip, zstd" {
		t.Fatalf("content encoding not preserved: %q", resp.Header.Get("Content-Encoding"))
	}

	reqs := waitForRequests(t, st, waitForSessionID(t, st))
	detail, err := st.GetRequest(context.Background(), reqs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ResponseBody != respJSON {
		t.Fatalf("stored response body was not decoded: %q", detail.ResponseBody)
	}
}

func TestProxyDropsRequestTrailers(t *testing.T) {
	var gotTrailer http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		gotTrailer = r.Trailer.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"m","choices":[],"usage":{}}`)
	}))
	defer upstream.Close()

	_, proxySrv := newTestProxy(t, upstream.URL, "openai_chat", "none")
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
		_ = pw.Close()
	}()
	req, _ := http.NewRequest(http.MethodPost, proxySrv.URL+"/open/v1/chat/completions", pr)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-Id", "trailer")
	req.Trailer = http.Header{"X-Checksum": []string{"abc"}}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if len(gotTrailer) != 0 {
		t.Fatalf("trailers were forwarded: %#v", gotTrailer)
	}
}

func TestProxyRejectsUpgradeRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called for an upgrade request")
	}))
	defer upstream.Close()

	_, proxySrv := newTestProxy(t, upstream.URL, "openai_chat", "none")
	req, _ := http.NewRequest(http.MethodGet, proxySrv.URL+"/open/v1/models", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", resp.StatusCode)
	}
}
