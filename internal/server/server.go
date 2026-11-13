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
	}
	s.api.HandleFunc("GET /api/sessions", s.handleSessions)
	s.api.HandleFunc("GET /api/sessions/{id}", s.handleSession)
	s.api.HandleFunc("GET /api/requests", s.handleRequests)
	s.api.HandleFunc("GET /api/requests/{id}", s.handleRequest)
	s.api.HandleFunc("GET /api/facets", s.handleFacets)
	s.api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/"):
		s.api.ServeHTTP(w, r)
	case s.cfg.MatchUpstream(r.URL.Path) != nil:
		s.proxy.ServeHTTP(w, r)
	default:
		s.serveStatic(w, r)
	}
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
