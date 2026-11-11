// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"dumbllmproxy/internal/config"
	"dumbllmproxy/internal/cost"
	"dumbllmproxy/internal/pricing"
	"dumbllmproxy/internal/proxy"
	"dumbllmproxy/internal/server"
	"dumbllmproxy/internal/store"
)

func TestAPIAndStatic(t *testing.T) {
	cfg := &config.Config{
		Proxy: config.ProxyConfig{Listen: "127.0.0.1:0", Database: filepath.Join(t.TempDir(), "test.db")},
		Upstreams: []config.UpstreamConfig{{
			Name: "test", Prefix: "/open", BaseURL: "https://example.com", Parser: "openai_chat", Pricing: "none",
		}},
		SessionHeaders: []string{"x-session-id"},
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.Proxy.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	started := time.Now().UTC().Add(-time.Minute)
	completed := started.Add(time.Second)
	reqBody := `{"model":"m","messages":[{"role":"system","content":"sys"},{"role":"user","content":"hi"}]}`
	respBody := `{"id":"r1","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`
	providerCost := 0.01
	id, err := st.SaveRequest(context.Background(), &store.RequestRecord{
		SessionKey:    "session-a",
		SessionHeader: "X-Session-Id",
		Conversation:  true,
		StartedAt:     started,
		CompletedAt:   &completed,
		Method:        "POST",
		LocalPath:     "/open/v1/chat/completions",
		UpstreamURL:   "https://example.com/v1/chat/completions",
		Provider:      "test",
		Model:         "m",
		StatusCode:    200,
		RequestBody:   []byte(reqBody),
		ResponseBody:  []byte(respBody),
		Parser:        "openai_chat",
		Pricing:       "openrouter",
		TotalTokens:   int64Ptr(6),
		ProviderCost:  &providerCost,
	})
	if err != nil {
		t.Fatal(err)
	}

	st.SetCostResolver(cost.NewResolver(pricing.NewRegistry()).Resolve)
	p := proxy.New(cfg, st, log.New(io.Discard, "", 0))
	srv, err := server.New(cfg, st, p)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	var sessions struct {
		Sessions []store.SessionSummary `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(sessions.Sessions) != 1 || sessions.Sessions[0].SessionKey != "session-a" {
		t.Fatalf("unexpected sessions: %#v", sessions.Sessions)
	}

	resp, err = http.Get(ts.URL + "/api/requests")
	if err != nil {
		t.Fatal(err)
	}
	var requestList struct {
		Requests []store.RequestSummary `json:"requests"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&requestList); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(requestList.Requests) != 1 || requestList.Requests[0].ID != id {
		t.Fatalf("unexpected request list: %#v", requestList.Requests)
	}

	since := url.QueryEscape(started.Add(-time.Hour).Format(time.RFC3339))
	resp, err = http.Get(ts.URL + "/api/requests?limit=1&offset=0&since=" + since)
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Requests []store.RequestSummary `json:"requests"`
		Total    int64                  `json:"total"`
		Limit    int                    `json:"limit"`
		Offset   int                    `json:"offset"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if page.Total != 1 || page.Limit != 1 || page.Offset != 0 || len(page.Requests) != 1 {
		t.Fatalf("unexpected filtered page: %#v", page)
	}

	resp, err = http.Get(ts.URL + "/api/requests?limit=1&offset=1")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if page.Total != 1 || len(page.Requests) != 0 {
		t.Fatalf("unexpected second page: %#v", page)
	}

	resp, err = http.Get(ts.URL + "/api/requests?since=not-a-date")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid since, got %d", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/api/sessions/" + strconv.FormatInt(sessions.Sessions[0].ID, 10))
	if err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Summary      store.SessionSummary   `json:"summary"`
		Requests     []store.RequestSummary `json:"requests"`
		Conversation struct {
			Roots []json.RawMessage `json:"roots"`
		} `json:"conversation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(detail.Requests) != 1 || len(detail.Conversation.Roots) == 0 {
		t.Fatalf("unexpected session detail: %#v", detail)
	}

	resp, err = http.Get(ts.URL + "/api/requests/" + strconv.FormatInt(id, 10))
	if err != nil {
		t.Fatal(err)
	}
	detailBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var reqDetail struct {
		store.RequestDetail
		Conversation struct {
			Roots []json.RawMessage `json:"roots"`
		} `json:"conversation"`
	}
	if err := json.Unmarshal(detailBody, &reqDetail); err != nil {
		t.Fatal(err)
	}
	if reqDetail.RequestBody != reqBody || reqDetail.ResponseBody != respBody {
		t.Fatalf("unexpected request detail: %#v", reqDetail)
	}
	if len(reqDetail.Conversation.Roots) == 0 {
		t.Fatal("expected request conversation roots")
	}
	// The conversation kind and the conversation tree must not share a JSON key.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(detailBody, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["is_conversation"]) != "true" {
		t.Fatalf("expected is_conversation=true, got %s", raw["is_conversation"])
	}
	if len(raw["conversation"]) == 0 || raw["conversation"][0] != '{' {
		t.Fatalf("expected conversation tree object, got %s", raw["conversation"])
	}

	resp, err = http.Get(ts.URL + "/api/nope")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown API, got %d", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "dumbllmproxy") {
		t.Fatalf("unexpected static index: %s", body)
	}
}

func int64Ptr(v int64) *int64 { return &v }

func TestStaticMissingAssetReturns404(t *testing.T) {
	cfg := &config.Config{
		Proxy: config.ProxyConfig{Listen: "127.0.0.1:0", Database: filepath.Join(t.TempDir(), "test.db")},
		Upstreams: []config.UpstreamConfig{{
			Name: "test", Prefix: "/open", BaseURL: "https://example.com", Parser: "none", Pricing: "none",
		}},
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.Proxy.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p := proxy.New(cfg, st, log.New(io.Discard, "", 0))
	srv, err := server.New(cfg, st, p)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/assets/does-not-exist.js")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing asset, got %d", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/some/client/route")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected SPA fallback for extensionless route, got %d", resp.StatusCode)
	}
}
