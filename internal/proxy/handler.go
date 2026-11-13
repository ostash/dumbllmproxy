// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"dumbllmproxy/internal/capture"
	"dumbllmproxy/internal/config"
	"dumbllmproxy/internal/openai"
	"dumbllmproxy/internal/store"
	"dumbllmproxy/internal/usage"
)

// maxResponseStreamBytes bounds the amount of a streaming response kept for
// reconstruction. Normal LLM streams are far smaller; a larger stream is
// treated as incomplete instead of growing memory without bound.
const maxResponseStreamBytes = 256 << 20

type Handler struct {
	cfg    *config.Config
	store  *store.Store
	client *http.Client
	logger *log.Logger
}

func New(cfg *config.Config, st *store.Store, logger *log.Logger) *Handler {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Preserve the upstream Content-Encoding bytes; capture decodes a copy.
	transport.DisableCompression = true
	transport.ForceAttemptHTTP2 = true
	transport.DialContext = (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext
	return &Handler{
		cfg:    cfg,
		store:  st,
		client: &http.Client{Transport: transport},
		logger: logger,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	up := h.cfg.MatchUpstream(r.URL.Path)
	if up == nil {
		http.NotFound(w, r)
		return
	}
	if isUpgradeRequest(r) {
		h.logger.Printf("rejected connection upgrade for %s", r.URL.Path)
		http.Error(w, "connection upgrades are not supported", http.StatusNotImplemented)
		return
	}

	startedAt := time.Now().UTC()
	rawRequestBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	decodedRequestBody, reqDecodeErr := capture.Decode(rawRequestBody, contentEncoding(r.Header))
	storedRequestBody := decodedRequestBody
	if reqDecodeErr != nil {
		storedRequestBody = rawRequestBody
	}

	model := openai.ExtractModel(storedRequestBody)
	sessionKey := h.session(r.Header)
	harness := h.harness(r.Header)
	metadata := extractHeaders(r.Header, h.cfg.MetadataHeaderMatchers(), "request")
	parseErrors := []string{}
	if reqDecodeErr != nil {
		parseErrors = append(parseErrors, "request decode: "+reqDecodeErr.Error())
	}

	targetURL, err := up.TargetURL(r.URL)
	if err != nil {
		h.fail(w, r, up, startedAt, storedRequestBody, model, sessionKey, harness, metadata, err, parseErrors)
		return
	}

	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL.String(), bytes.NewReader(rawRequestBody))
	if err != nil {
		h.fail(w, r, up, startedAt, storedRequestBody, model, sessionKey, harness, metadata, err, parseErrors)
		return
	}
	copyRequestHeaders(upReq.Header, r.Header)
	upReq.Host = targetURL.Host

	resp, err := h.client.Do(upReq)
	if err != nil {
		h.fail(w, r, up, startedAt, storedRequestBody, model, sessionKey, harness, metadata, err, parseErrors)
		return
	}
	defer resp.Body.Close()

	addHeaderMetadata(metadata, resp.Header, h.cfg.MetadataHeaderMatchers(), "response")
	copyResponseHeaders(w.Header(), resp.Header)

	streamed := isEventStream(resp.Header.Get("Content-Type"))
	var completedAt *time.Time
	var responseBody, rawResponseBody []byte
	var incomplete bool
	var streamParseError string

	if streamed {
		w.WriteHeader(resp.StatusCode)
		flusher, _ := w.(http.Flusher)
		var rawStream bytes.Buffer
		buf := make([]byte, 32*1024)
		clientGone := false
		var readErr error
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				if rawStream.Len()+n > maxResponseStreamBytes {
					readErr = fmt.Errorf("response stream exceeds %d bytes", maxResponseStreamBytes)
					break
				}
				rawStream.Write(buf[:n])
				if !clientGone {
					if _, werr := w.Write(buf[:n]); werr != nil {
						clientGone = true
					}
					if flusher != nil {
						flusher.Flush()
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					readErr = err
				}
				break
			}
		}
		now := time.Now().UTC()
		completedAt = &now

		streamComplete := false
		decodedStream, decodeErr := capture.Decode(rawStream.Bytes(), contentEncoding(resp.Header))
		if decodeErr != nil {
			responseBody = rawStream.Bytes()
			rawResponseBody = rawStream.Bytes()
			incomplete = true
			streamParseError = "response decode: " + decodeErr.Error()
		} else if up.Parser == "openai_chat" {
			recon := openai.NewStreamReconstructor()
			recon.Feed(decodedStream)
			reconBody, complete, parseErr := recon.Finish()
			streamComplete = complete && parseErr == nil
			if reconBody != nil {
				responseBody = reconBody
			} else {
				responseBody = decodedStream
			}
			if !complete || parseErr != nil {
				rawResponseBody = decodedStream
				incomplete = true
			}
			if parseErr != nil {
				streamParseError = parseErr.Error()
			}
		} else {
			// Unknown stream format: preserve the decoded stream as one raw blob.
			responseBody = decodedStream
		}
		// A read error after a complete stream is usually the client closing
		// the connection after [DONE]. The logical response is already
		// complete, so it must not be marked incomplete.
		if readErr != nil && !streamComplete {
			incomplete = true
			if streamParseError == "" {
				streamParseError = "stream read: " + readErr.Error()
			}
		}
	} else {
		raw, readErr := io.ReadAll(resp.Body)
		now := time.Now().UTC()
		completedAt = &now
		if readErr != nil {
			incomplete = true
			streamParseError = "response read: " + readErr.Error()
		}
		decoded, decodeErr := capture.Decode(raw, contentEncoding(resp.Header))
		responseBody = decoded
		if decodeErr != nil {
			responseBody = raw
			if streamParseError == "" {
				streamParseError = "response decode: " + decodeErr.Error()
			}
		}
		w.WriteHeader(resp.StatusCode)
		if len(raw) > 0 {
			if _, err := w.Write(raw); err != nil {
				// The response was already sent as far as the client is concerned.
				h.logger.Printf("client write failed for %s: %v", r.URL.Path, err)
			}
		}
	}

	if streamParseError != "" {
		parseErrors = append(parseErrors, streamParseError)
	}

	// Auxiliary traffic (model listings, presets, health checks) is not stored.
	// It is classified here, after the response is available, and discarded.
	if !openai.IsConversationRequest(storedRequestBody, responseBody) {
		return
	}

	captured := h.capture(up, storedRequestBody, responseBody, startedAt)
	if rawResponseBody != nil {
		captured.RawResponseBody = rawResponseBody
	}
	captured.Incomplete = incomplete
	captured.ParseError = strings.Join(parseErrors, "; ")

	rec := &store.RequestRecord{
		SessionKey:          sessionKey,
		Harness:             harness,
		StartedAt:           startedAt,
		CompletedAt:         completedAt,
		Method:              r.Method,
		Endpoint:            requestEndpoint(up, r),
		Provider:            up.Name,
		Model:               captured.Model,
		StatusCode:          resp.StatusCode,
		RequestContentType:  r.Header.Get("Content-Type"),
		ResponseContentType: resp.Header.Get("Content-Type"),
		MetadataJSON:        marshalJSON(metadata),
		RequestBody:         captured.RequestBody,
		ResponseBody:        captured.ResponseBody,
		RawResponseBody:     captured.RawResponseBody,
		Streamed:            streamed,
		Incomplete:          captured.Incomplete,
		HasToolCalls:        captured.HasToolCalls,
		Parser:              up.Parser,
		Pricing:             up.Pricing,
		ParseError:          captured.ParseError,
		UsageJSON:           captured.UsageJSON,
		InputTokens:         captured.Usage.InputTokens,
		OutputTokens:        captured.Usage.OutputTokens,
		CachedInputTokens:   captured.Usage.CachedInputTokens,
		CacheReadTokens:     captured.Usage.CacheReadTokens,
		CacheWriteTokens:    captured.Usage.CacheWriteTokens,
		ReasoningTokens:     captured.Usage.ReasoningTokens,
		TotalTokens:         captured.Usage.TotalTokens,
		ProviderCost:        captured.ProviderCost,
	}
	h.save(rec)
}

type captureResult struct {
	RequestBody     []byte
	ResponseBody    []byte
	RawResponseBody []byte
	Model           string
	Incomplete      bool
	HasToolCalls    bool
	ParseError      string
	Usage           usage.Usage
	UsageJSON       string
	ProviderCost    *float64
}

func (h *Handler) capture(up *config.UpstreamConfig, requestBody, responseBody []byte, startedAt time.Time) captureResult {
	result := captureResult{
		RequestBody:  requestBody,
		ResponseBody: responseBody,
		Model:        openai.ExtractModel(requestBody),
	}
	if up.Parser == "openai_chat" {
		result.HasToolCalls = openai.HasToolCalls(responseBody)
	}
	if u, ok := usage.ExtractFromResponse(responseBody); ok {
		result.Usage = u
		result.UsageJSON = marshalJSON(u.Raw)
		// Provider-reported cost is a captured fact. Calculated costs are not
		// stored; they are recomputed from these facts on every read.
		if u.ProviderCost != nil {
			result.ProviderCost = u.ProviderCost
		}
	}
	return result
}

func (h *Handler) session(header http.Header) string {
	for _, name := range h.cfg.SessionHeaders {
		if name == "" {
			continue
		}
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func (h *Handler) harness(header http.Header) string {
	for _, name := range h.cfg.HarnessHeaders {
		if name == "" {
			continue
		}
		value := strings.TrimSpace(header.Get(name))
		if value == "" {
			continue
		}
		if strings.EqualFold(name, "user-agent") {
			return harnessFromUserAgent(value)
		}
		return value
	}
	return ""
}

func harnessFromUserAgent(ua string) string {
	fields := strings.Fields(strings.TrimSpace(ua))
	if len(fields) == 0 {
		return ""
	}
	name := fields[0]
	if i := strings.IndexByte(name, '/'); i > 0 {
		name = name[:i]
	}
	return name
}

// requestEndpoint returns the client path relative to the matched upstream
// prefix, including the query string. The prefix itself is routing detail and
// is not stored; provider identifies the upstream.
func requestEndpoint(up *config.UpstreamConfig, r *http.Request) string {
	endpoint := up.RemainingPath(r.URL.Path)
	if r.URL.RawQuery != "" {
		endpoint += "?" + r.URL.RawQuery
	}
	return endpoint
}

// contentEncoding joins repeated Content-Encoding headers so nested encodings
// declared on separate header lines are decoded in the right order.
func contentEncoding(header http.Header) string {
	return strings.Join(header.Values("Content-Encoding"), ",")
}

func (h *Handler) save(rec *store.RequestRecord) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := h.store.SaveRequest(ctx, rec); err != nil {
		h.logger.Printf("failed to save request: %v", err)
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, up *config.UpstreamConfig, startedAt time.Time, storedRequestBody []byte, model, sessionKey, harness string, metadata map[string]any, cause error, parseErrors []string) {
	completed := time.Now().UTC()
	body := []byte("upstream request failed: " + cause.Error())
	h.logger.Printf("upstream request failed for %s: %v", r.URL.Path, cause)
	if !openai.IsConversationRequest(storedRequestBody, nil) {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	parseErrors = append(parseErrors, "upstream: "+cause.Error())
	rec := &store.RequestRecord{
		SessionKey:         sessionKey,
		Harness:            harness,
		StartedAt:          startedAt,
		CompletedAt:        &completed,
		Method:             r.Method,
		Endpoint:           requestEndpoint(up, r),
		Provider:           up.Name,
		Model:              model,
		StatusCode:         http.StatusBadGateway,
		RequestContentType: r.Header.Get("Content-Type"),
		MetadataJSON:       marshalJSON(metadata),
		RequestBody:        storedRequestBody,
		ResponseBody:       body,
		Parser:             up.Parser,
		Pricing:            up.Pricing,
		ParseError:         strings.Join(parseErrors, "; "),
	}
	h.save(rec)
	http.Error(w, "upstream request failed", http.StatusBadGateway)
}

func extractHeaders(header http.Header, matchers []config.HeaderMatcher, prefix string) map[string]any {
	out := map[string]any{}
	for _, m := range matchers {
		if m.Pattern != nil {
			for name, values := range header {
				if m.Pattern.MatchString(name) {
					out[prefix+"."+http.CanonicalHeaderKey(name)] = strings.Join(values, ", ")
				}
			}
			continue
		}
		if m.Name == "" {
			continue
		}
		values := header.Values(m.Name)
		if len(values) == 0 {
			continue
		}
		out[prefix+"."+http.CanonicalHeaderKey(m.Name)] = strings.Join(values, ", ")
	}
	return out
}

func addHeaderMetadata(dst map[string]any, header http.Header, matchers []config.HeaderMatcher, prefix string) {
	for k, v := range extractHeaders(header, matchers, prefix) {
		dst[k] = v
	}
}

func marshalJSON(v any) string {
	if v == nil {
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

func isEventStream(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

func copyRequestHeaders(dst, src http.Header) {
	copyEndToEndHeaders(dst, src)
	// The body is already buffered, so the expectation has been satisfied by
	// this proxy. Forwarding it would make the upstream wait for a 100 that is
	// no longer needed.
	dst.Del("Expect")
}

// isUpgradeRequest reports whether the client asked to switch protocols. The
// proxy cannot tunnel an upgraded connection, so such requests are rejected
// instead of being forwarded with the upgrade headers silently stripped.
func isUpgradeRequest(r *http.Request) bool {
	if r.Header.Get("Upgrade") != "" {
		return true
	}
	for _, value := range r.Header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
				return true
			}
		}
	}
	return false
}

func copyResponseHeaders(dst, src http.Header) {
	copyEndToEndHeaders(dst, src)
}

// copyEndToEndHeaders forwards ordinary headers and repeated values while
// dropping hop-by-hop headers. Per RFC 7230 section 6.1, every header named by
// a Connection header value is connection-specific too, so it is dropped as
// well. Header names are compared case-insensitively.
func copyEndToEndHeaders(dst, src http.Header) {
	connectionSpecific := connectionNominatedHeaders(src)
	for k, values := range src {
		canonical := http.CanonicalHeaderKey(k)
		if isHopByHop(canonical) || connectionSpecific[canonical] {
			continue
		}
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

// connectionNominatedHeaders returns the canonical names listed in every
// Connection header value. Multiple Connection lines and comma-separated
// lists are both supported.
func connectionNominatedHeaders(header http.Header) map[string]bool {
	var nominated map[string]bool
	for _, value := range header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if nominated == nil {
				nominated = map[string]bool{}
			}
			nominated[http.CanonicalHeaderKey(name)] = true
		}
	}
	return nominated
}

func isHopByHop(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	default:
		return false
	}
}
