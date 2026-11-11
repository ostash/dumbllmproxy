// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package config

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the root of the human-editable YAML configuration file.
type Config struct {
	Proxy           ProxyConfig      `yaml:"proxy"`
	Upstreams       []UpstreamConfig `yaml:"upstreams"`
	SessionHeaders  []string         `yaml:"session_headers"`
	MetadataHeaders []string         `yaml:"metadata_headers"`
	Pricing         PricingConfig    `yaml:"pricing"`
}

type ProxyConfig struct {
	Listen   string `yaml:"listen"`
	Database string `yaml:"database"`
}

type UpstreamConfig struct {
	Name    string `yaml:"name"`
	Prefix  string `yaml:"prefix"`
	BaseURL string `yaml:"base_url"`
	// Parser selects the response/stream parser. Supported: openai_chat, none.
	Parser string `yaml:"parser"`
	// Pricing selects cost handling. Supported: openrouter, deepseek, none.
	Pricing string `yaml:"pricing"`
}

type PricingConfig struct {
	DeepSeek *DeepSeekPricing `yaml:"deepseek"`
}

type DeepSeekPricing struct {
	Unit             string                          `yaml:"unit"`
	ScheduleTimezone string                          `yaml:"schedule_timezone"`
	HolidayTimezone  string                          `yaml:"holiday_timezone"`
	Holidays         []string                        `yaml:"holidays"`
	Models           map[string]DeepSeekModelPricing `yaml:"models"`
}

type DeepSeekModelPricing struct {
	Rates []PricingRate `yaml:"rates"`
}

type TimeWindow struct {
	Start string `yaml:"start"`
	End   string `yaml:"end"`
}

type RateConditions struct {
	From         string       `yaml:"from"`
	To           string       `yaml:"to"`
	Days         []string     `yaml:"days"`
	TimeWindows  []TimeWindow `yaml:"time_windows"`
	Holiday      *bool        `yaml:"holiday"`
	Dates        []string     `yaml:"dates"`
	ExcludeDates []string     `yaml:"exclude_dates"`
}

type PricingRate struct {
	Name           string          `yaml:"name"`
	Conditions     *RateConditions `yaml:"conditions"`
	InputCacheHit  float64         `yaml:"input_cache_hit"`
	InputCacheMiss float64         `yaml:"input_cache_miss"`
	Output         float64         `yaml:"output"`
}

type RateCard struct {
	InputCacheHit  float64 `yaml:"input_cache_hit"`
	InputCacheMiss float64 `yaml:"input_cache_miss"`
	Output         float64 `yaml:"output"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) SetDefaults() {
	if c.Proxy.Listen == "" {
		c.Proxy.Listen = "127.0.0.1:8080"
	}
	if c.Proxy.Database == "" {
		c.Proxy.Database = "dumbllmproxy.db"
	}
	for i := range c.Upstreams {
		u := &c.Upstreams[i]
		u.Prefix = normalizePrefix(u.Prefix)
		u.Parser = strings.ToLower(strings.TrimSpace(u.Parser))
		if u.Parser == "" {
			u.Parser = "none"
		}
		u.Pricing = strings.ToLower(strings.TrimSpace(u.Pricing))
		if u.Pricing == "" {
			u.Pricing = "none"
		}
		if u.Name == "" {
			u.Name = strings.Trim(u.Prefix, "/")
			if u.Name == "" {
				u.Name = "default"
			}
		}
	}
	for i := range c.SessionHeaders {
		c.SessionHeaders[i] = strings.TrimSpace(c.SessionHeaders[i])
	}
	for i := range c.MetadataHeaders {
		c.MetadataHeaders[i] = strings.TrimSpace(c.MetadataHeaders[i])
	}
	if c.Pricing.DeepSeek != nil {
		d := c.Pricing.DeepSeek
		if d.Unit == "" {
			d.Unit = "per_million_tokens"
		}
		if d.ScheduleTimezone == "" {
			d.ScheduleTimezone = "UTC"
		}
		if d.HolidayTimezone == "" {
			d.HolidayTimezone = d.ScheduleTimezone
		}
	}
	sort.SliceStable(c.Upstreams, func(i, j int) bool {
		return len(c.Upstreams[i].Prefix) > len(c.Upstreams[j].Prefix)
	})
}

func normalizePrefix(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if p != "/" {
		p = strings.TrimRight(p, "/")
	}
	return p
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.Proxy.Listen) == "" {
		return fmt.Errorf("proxy.listen must not be empty")
	}
	if strings.TrimSpace(c.Proxy.Database) == "" {
		return fmt.Errorf("proxy.database must not be empty")
	}
	seenPrefixes := map[string]bool{}
	for i := range c.Upstreams {
		u := &c.Upstreams[i]
		if !strings.HasPrefix(u.Prefix, "/") {
			return fmt.Errorf("upstream %q prefix must start with /", u.Name)
		}
		if u.Prefix == "/" {
			return fmt.Errorf("upstream %q prefix / is not allowed; it would shadow the UI", u.Name)
		}
		if u.Prefix == "/api" || strings.HasPrefix(u.Prefix, "/api/") {
			return fmt.Errorf("upstream %q prefix /api is reserved for the UI API", u.Name)
		}
		if seenPrefixes[u.Prefix] {
			return fmt.Errorf("duplicate upstream prefix %q", u.Prefix)
		}
		seenPrefixes[u.Prefix] = true
		parsed, err := url.Parse(u.BaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("upstream %q has invalid base_url %q", u.Name, u.BaseURL)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("upstream %q base_url must use http or https", u.Name)
		}
		switch u.Parser {
		case "openai_chat", "none":
		default:
			return fmt.Errorf("upstream %q has unsupported parser %q", u.Name, u.Parser)
		}
		switch u.Pricing {
		case "openrouter", "deepseek", "none":
		default:
			return fmt.Errorf("upstream %q has unsupported pricing %q", u.Name, u.Pricing)
		}
		if u.Pricing == "deepseek" {
			if c.Pricing.DeepSeek == nil {
				return fmt.Errorf("upstream %q uses pricing deepseek but pricing.deepseek is missing", u.Name)
			}
			if len(c.Pricing.DeepSeek.Models) == 0 {
				return fmt.Errorf("pricing.deepseek.models must define at least one model")
			}
		}
	}
	return nil
}

// MatchUpstream returns the longest matching upstream for a request path.
func (c *Config) MatchUpstream(path string) *UpstreamConfig {
	for i := range c.Upstreams {
		if c.Upstreams[i].Matches(path) {
			return &c.Upstreams[i]
		}
	}
	return nil
}

func (u *UpstreamConfig) Matches(path string) bool {
	if u.Prefix == "/" {
		return true
	}
	return path == u.Prefix || strings.HasPrefix(path, u.Prefix+"/")
}

func (u *UpstreamConfig) RemainingPath(path string) string {
	if u.Prefix == "/" {
		return path
	}
	rest := strings.TrimPrefix(path, u.Prefix)
	if rest == "" {
		return ""
	}
	if !strings.HasPrefix(rest, "/") {
		return ""
	}
	return rest
}

// TargetURL builds the upstream URL for an incoming request URL.
func (u *UpstreamConfig) TargetURL(in *url.URL) (*url.URL, error) {
	base, err := url.Parse(u.BaseURL)
	if err != nil {
		return nil, err
	}
	rest := u.RemainingPath(in.Path)
	target := *base
	target.Path = joinURLPath(base.Path, rest)
	target.RawPath = ""
	if in.RawQuery != "" {
		target.RawQuery = in.RawQuery
	}
	return &target, nil
}

func joinURLPath(base, rest string) string {
	if rest == "" {
		if base == "" {
			return "/"
		}
		return base
	}
	if base == "" {
		return rest
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(rest, "/")
}
