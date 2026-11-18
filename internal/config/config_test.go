// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package config

import (
	"net/url"
	"testing"
)

func TestTargetURLPrefixMapping(t *testing.T) {
	u := UpstreamConfig{
		Name:    "openrouter",
		Prefix:  "/openrouter",
		BaseURL: "https://openrouter.ai/api",
		Parser:  "openai_chat",
		Pricing: "openrouter",
	}
	in, _ := url.Parse("http://localhost:8080/openrouter/v1/chat/completions?x=1")
	out, err := u.TargetURL(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "https://openrouter.ai/api/v1/chat/completions?x=1" {
		t.Fatalf("unexpected target: %s", out)
	}
}

func TestTargetURLPrefixRoot(t *testing.T) {
	u := UpstreamConfig{Prefix: "/", BaseURL: "http://example.com/api"}
	in, _ := url.Parse("http://localhost/foo/bar?q=1")
	out, err := u.TargetURL(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "http://example.com/api/foo/bar?q=1" {
		t.Fatalf("unexpected target: %s", out)
	}
}

func TestTargetURLPreservesEncodedPath(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		baseURL string
		in      string
		want    string
	}{
		{
			name: "encoded slash", prefix: "/open", baseURL: "https://example.com/api",
			in: "http://localhost/open/a%2Fb?x=1", want: "https://example.com/api/a%2Fb?x=1",
		},
		{
			name: "lowercase encoded slash", prefix: "/open", baseURL: "https://example.com/api",
			in: "http://localhost/open/a%2fb", want: "https://example.com/api/a%2fb",
		},
		{
			name: "encoded separator", prefix: "/open", baseURL: "https://example.com/api",
			in: "http://localhost/open%2Ffoo", want: "https://example.com/api%2Ffoo",
		},
		{
			name: "root prefix", prefix: "/", baseURL: "http://example.com/api",
			in: "http://localhost/a%2Fb", want: "http://example.com/api/a%2Fb",
		},
		{
			name: "encoded base path", prefix: "/open", baseURL: "https://example.com/base%2Fv1",
			in: "http://localhost/open/a%2Fb", want: "https://example.com/base%2Fv1/a%2Fb",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := UpstreamConfig{Prefix: tc.prefix, BaseURL: tc.baseURL}
			in, err := url.Parse(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			out, err := u.TargetURL(in)
			if err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != tc.want {
				t.Fatalf("TargetURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMatchUpstreamLongestPrefix(t *testing.T) {
	cfg := &Config{
		Upstreams: []UpstreamConfig{
			{Prefix: "/"},
			{Prefix: "/open"},
			{Prefix: "/open/deepseek"},
		},
	}
	cfg.SetDefaults()
	if got := cfg.MatchUpstream("/open/deepseek/v1/chat"); got == nil || got.Prefix != "/open/deepseek" {
		t.Fatalf("unexpected match: %#v", got)
	}
	if got := cfg.MatchUpstream("/open/v1/chat"); got == nil || got.Prefix != "/open" {
		t.Fatalf("unexpected match: %#v", got)
	}
	if got := cfg.MatchUpstream("/other"); got == nil || got.Prefix != "/" {
		t.Fatalf("unexpected match: %#v", got)
	}
}

func TestValidateRejectsBadParser(t *testing.T) {
	cfg := &Config{
		Proxy: ProxyConfig{Listen: "127.0.0.1:0", Database: "x.db"},
		Upstreams: []UpstreamConfig{{
			Name: "x", Prefix: "/x", BaseURL: "https://example.com", Parser: "bogus", Pricing: "none",
		}},
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestExampleConfigLoads(t *testing.T) {
	cfg, err := Load("../../config.example.yaml")
	if err != nil {
		t.Fatalf("example config does not load: %v", err)
	}
	if len(cfg.Upstreams) < 2 {
		t.Fatalf("expected example upstreams, got %d", len(cfg.Upstreams))
	}
	if cfg.Pricing.DeepSeek == nil {
		t.Fatal("expected deepseek pricing in example")
	}
}

func TestValidateRejectsRootPrefix(t *testing.T) {
	cfg := &Config{
		Proxy: ProxyConfig{Listen: "127.0.0.1:0", Database: "x.db"},
		Upstreams: []UpstreamConfig{{
			Name: "root", Prefix: "/", BaseURL: "https://example.com", Parser: "none", Pricing: "none",
		}},
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for root prefix")
	}
}

func TestValidateRejectsDuplicateNames(t *testing.T) {
	cfg := &Config{
		Proxy: ProxyConfig{Listen: "127.0.0.1:0", Database: "x.db"},
		Upstreams: []UpstreamConfig{
			{Name: "same", Prefix: "/a", BaseURL: "https://example.com", Parser: "none", Pricing: "none"},
			{Name: "same", Prefix: "/b", BaseURL: "https://example.com", Parser: "none", Pricing: "none"},
		},
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for duplicate names")
	}
}

func TestMetadataHeaderMatchers(t *testing.T) {
	cfg := &Config{
		Proxy: ProxyConfig{Listen: "127.0.0.1:0", Database: "x.db"},
		Upstreams: []UpstreamConfig{{
			Name: "x", Prefix: "/x", BaseURL: "https://example.com", Parser: "none", Pricing: "none",
		}},
		MetadataHeaders: []string{"x-request-id", "re:^x-trace-"},
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	matchers := cfg.MetadataHeaderMatchers()
	if len(matchers) != 2 {
		t.Fatalf("expected 2 matchers, got %d", len(matchers))
	}
	if matchers[0].Pattern != nil || matchers[0].Name != "x-request-id" {
		t.Fatalf("unexpected exact matcher: %#v", matchers[0])
	}
	if matchers[1].Pattern == nil || !matchers[1].Pattern.MatchString("X-Trace-Id") {
		t.Fatalf("unexpected regex matcher: %#v", matchers[1])
	}
}

func TestMaxRequestBytesDefaultAndValidation(t *testing.T) {
	cfg := &Config{}
	cfg.SetDefaults()
	if cfg.Proxy.MaxRequestBytes != DefaultMaxRequestBytes {
		t.Fatalf("max request bytes = %d, want %d", cfg.Proxy.MaxRequestBytes, DefaultMaxRequestBytes)
	}
	cfg.Proxy.MaxRequestBytes = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for negative max_request_bytes")
	}
}

func TestValidateRejectsBadMetadataRegex(t *testing.T) {
	cfg := &Config{
		Proxy: ProxyConfig{Listen: "127.0.0.1:0", Database: "x.db"},
		Upstreams: []UpstreamConfig{{
			Name: "x", Prefix: "/x", BaseURL: "https://example.com", Parser: "none", Pricing: "none",
		}},
		MetadataHeaders: []string{"re:["},
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}
