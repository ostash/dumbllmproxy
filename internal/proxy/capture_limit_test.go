// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package proxy

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dumbllmproxy/internal/config"
	"dumbllmproxy/internal/store"
)

func TestCaptureLimitDoesNotStopForwarding(t *testing.T) {
	upstreamBody := `{"choices":[{"message":{"role":"assistant","content":"` + strings.Repeat("x", 1024) + `"}}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstreamBody)
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Proxy: config.ProxyConfig{Listen: "127.0.0.1:0", Database: filepath.Join(t.TempDir(), "test.db")},
		Upstreams: []config.UpstreamConfig{{
			Name: "test", Prefix: "/open", BaseURL: upstream.URL, Parser: "openai_chat", Pricing: "none",
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
	h := New(cfg, st, log.New(io.Discard, "", 0))
	h.captureLimit = 64
	srv := httptest.NewServer(h)
	defer h.Close()
	defer srv.Close()

	reqBody := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(srv.URL+"/open/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != upstreamBody {
		t.Fatalf("client body was truncated: got %d bytes, want %d", len(got), len(upstreamBody))
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		reqs, err := st.ListAllRequests(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(reqs) > 0 {
			if !reqs[0].Incomplete {
				t.Fatal("capture was not marked incomplete")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for capture")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
