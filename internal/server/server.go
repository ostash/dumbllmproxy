// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"dumbllmproxy/internal/config"
	"dumbllmproxy/internal/conversation"
	"dumbllmproxy/internal/proxy"
	"dumbllmproxy/internal/store"
	"dumbllmproxy/web"
)

type Server struct {
	cfg    *config.Config
	store  *store.Store
	proxy  *proxy.Handler
	api    *http.ServeMux
	static http.Handler
	fsys   fs.FS
	// hasIndex is false when the frontend was never built (only the committed
	// placeholder is embedded). The UI then explains how to build it instead of
	// returning a bare 404.
	hasIndex bool
	localIPs []net.IP
}

func New(cfg *config.Config, st *store.Store, p *proxy.Handler) (*Server, error) {
	sub, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return nil, err
	}
	_, indexErr := fs.Stat(sub, "index.html")
	s := &Server{
		cfg:      cfg,
		store:    st,
		proxy:    p,
		api:      http.NewServeMux(),
		static:   http.FileServer(http.FS(sub)),
		fsys:     sub,
		hasIndex: indexErr == nil,
		localIPs: localInterfaceIPs(),
	}
	s.api.HandleFunc("GET /api/sessions", s.handleSessions)
	s.api.HandleFunc("GET /api/sessions/{id}", s.handleSession)
	s.api.HandleFunc("GET /api/requests", s.handleRequests)
	s.api.HandleFunc("GET /api/requests/{id}", s.handleRequest)
	s.api.HandleFunc("DELETE /api/requests/{id}", s.handleDeleteRequest)
	s.api.HandleFunc("POST /api/requests/delete", s.handleDeleteRequests)
	s.api.HandleFunc("GET /api/facets", s.handleFacets)
	s.api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.hostAllowed(r.Host) {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	switch {
	case r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/"):
		s.api.ServeHTTP(w, r)
	case s.cfg.MatchUpstream(r.URL.Path) != nil:
		s.proxy.ServeHTTP(w, r)
	default:
		s.serveStatic(w, r)
	}
}

// hostAllowed reports whether the Host header names this machine. Accepting
// only localhost and local interface addresses blocks DNS rebinding while
// still allowing access through a LAN address. It is not authentication.
func (s *Server) hostAllowed(hostport string) bool {
	host := hostOnly(hostport)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if i := strings.LastIndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, local := range s.localIPs {
		if local.Equal(ip) {
			return true
		}
	}
	return false
}

func hostOnly(hostport string) string {
	hostport = strings.TrimSpace(hostport)
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	}
	return strings.TrimSuffix(strings.Trim(hostport, "[]"), ".")
}

func localInterfaceIPs() []net.IP {
	seen := map[string]struct{}{}
	var out []net.IP
	add := func(ip net.IP) {
		if ip == nil {
			return
		}
		key := ip.String()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, ip)
	}
	add(net.IPv4(127, 0, 0, 1))
	add(net.IPv6loopback)
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			switch v := addr.(type) {
			case *net.IPNet:
				add(v.IP)
			case *net.IPAddr:
				add(v.IP)
			}
		}
	}
	return out
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	opts, err := parseListOptions(r, "last_activity", map[string]bool{
		"last_activity": true,
		"session_key":   true,
		"request_count": true,
		"total_tokens":  true,
		"cost_amount":   true,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sessions, total, err := s.store.ListSessionsFiltered(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sessions == nil {
		sessions = []store.SessionSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": sessions,
		"total":    total,
		"limit":    opts.Limit,
		"offset":   opts.Offset,
	})
}

type sessionResponse struct {
	Summary      *store.SessionSummary  `json:"summary"`
	Requests     []store.RequestSummary `json:"requests"`
	Conversation conversation.Tree      `json:"conversation"`
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	summary, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if summary == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	requests, err := s.store.ListRequests(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if requests == nil {
		requests = []store.RequestSummary{}
	}
	treeRequests, err := s.store.ListRequestsForTree(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tree := conversation.Build(treeRequests)
	if tree.Roots == nil {
		tree.Roots = []*conversation.Node{}
	}
	writeJSON(w, http.StatusOK, sessionResponse{
		Summary:      summary,
		Requests:     requests,
		Conversation: tree,
	})
}

func (s *Server) handleFacets(w http.ResponseWriter, r *http.Request) {
	facets, err := s.store.Facets(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, facets)
}

func (s *Server) handleRequests(w http.ResponseWriter, r *http.Request) {
	opts, err := parseListOptions(r, "started_at", map[string]bool{
		"started_at":   true,
		"id":           true,
		"status_code":  true,
		"total_tokens": true,
		"cost_amount":  true,
		"duration":     true,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	requests, total, err := s.store.ListAllRequestsFiltered(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if requests == nil {
		requests = []store.RequestSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"requests": requests,
		"total":    total,
		"limit":    opts.Limit,
		"offset":   opts.Offset,
	})
}

func parseListOptions(r *http.Request, defaultSort string, allowedSorts map[string]bool) (store.ListOptions, error) {
	q := r.URL.Query()
	opts := store.ListOptions{Limit: 50, Sort: defaultSort, Desc: true}
	if value := strings.TrimSpace(q.Get("limit")); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil {
			return opts, fmt.Errorf("invalid limit")
		}
		if n <= 0 {
			n = 50
		}
		if n > 200 {
			n = 200
		}
		opts.Limit = n
	}
	if value := strings.TrimSpace(q.Get("offset")); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return opts, fmt.Errorf("invalid offset")
		}
		opts.Offset = n
	}
	if value := strings.TrimSpace(q.Get("sort")); value != "" {
		if !allowedSorts[value] {
			return opts, fmt.Errorf("invalid sort %q", value)
		}
		opts.Sort = value
	}
	switch strings.ToLower(strings.TrimSpace(q.Get("order"))) {
	case "", "desc":
		opts.Desc = true
	case "asc":
		opts.Desc = false
	default:
		return opts, fmt.Errorf("invalid order")
	}
	if value := strings.TrimSpace(q.Get("since")); value != "" {
		t, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			t, err = time.Parse(time.RFC3339, value)
		}
		if err != nil {
			return opts, fmt.Errorf("invalid since")
		}
		opts.Since = &t
	}
	opts.Query = strings.TrimSpace(q.Get("q"))
	switch strings.ToLower(strings.TrimSpace(q.Get("status"))) {
	case "":
	case "failed":
		opts.Failed = true
	default:
		return opts, fmt.Errorf("invalid status")
	}
	for name, target := range map[string]**bool{
		"incomplete": &opts.Incomplete,
		"tool_calls": &opts.HasToolCalls,
		"unassigned": &opts.Unassigned,
	} {
		value, err := parseBoolParam(q.Get(name))
		if err != nil {
			return opts, fmt.Errorf("invalid %s", name)
		}
		*target = value
	}
	opts.Provider = strings.TrimSpace(q.Get("provider"))
	opts.Model = strings.TrimSpace(q.Get("model"))
	opts.Harness = strings.TrimSpace(q.Get("harness"))
	for name, target := range map[string]**bool{
		"has_failures":   &opts.HasFailures,
		"has_incomplete": &opts.HasIncomplete,
	} {
		value, err := parseBoolParam(q.Get(name))
		if err != nil {
			return opts, fmt.Errorf("invalid %s", name)
		}
		*target = value
	}
	return opts, nil
}

func parseBoolParam(value string) (*bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return nil, nil
	case "1", "true", "yes":
		v := true
		return &v, nil
	case "0", "false", "no":
		v := false
		return &v, nil
	default:
		return nil, fmt.Errorf("invalid boolean")
	}
}

type requestResponse struct {
	*store.RequestDetail
	Conversation conversation.Tree `json:"conversation"`
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request id")
		return
	}
	detail, err := s.store.GetRequest(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "request not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tree := conversation.Build([]store.RequestForTree{{
		ID:           detail.ID,
		StartedAt:    detail.StartedAt,
		Parser:       detail.Parser,
		RequestBody:  []byte(detail.RequestBody),
		ResponseBody: []byte(detail.ResponseBody),
	}})
	if tree.Roots == nil {
		tree.Roots = []*conversation.Node{}
	}
	writeJSON(w, http.StatusOK, requestResponse{
		RequestDetail: detail,
		Conversation:  tree,
	})
}

func (s *Server) handleDeleteRequest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request id")
		return
	}
	deleted, err := s.store.DeleteRequest(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const maxBulkDeleteIDs = 1000

func (s *Server) handleDeleteRequests(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids must not be empty")
		return
	}
	seen := make(map[int64]struct{}, len(body.IDs))
	ids := make([]int64, 0, len(body.IDs))
	for _, id := range body.IDs {
		if id <= 0 {
			writeError(w, http.StatusBadRequest, "invalid request id")
			return
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) > maxBulkDeleteIDs {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d ids are allowed", maxBulkDeleteIDs))
		return
	}
	deleted, err := s.store.DeleteRequests(r.Context(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if !s.hasIndex {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "The web UI has not been built.\n\nRun `make frontend` (or `npm install && npm run build` in web/) and restart.\n")
		return
	}
	clean := path.Clean("/" + r.URL.Path)
	name := strings.TrimPrefix(clean, "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	info, err := fs.Stat(s.fsys, name)
	if err != nil {
		// Missing files with an extension are real 404s; extensionless paths
		// are client-side routes and fall back to the SPA entry point.
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		clone := r.Clone(r.Context())
		clone.URL.Path = "/"
		s.static.ServeHTTP(w, clone)
		return
	}
	if info.IsDir() {
		clone := r.Clone(r.Context())
		clone.URL.Path = "/"
		s.static.ServeHTTP(w, clone)
		return
	}
	s.static.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}
