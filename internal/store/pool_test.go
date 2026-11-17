// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"dumbllmproxy/internal/store"
)

// TestPooledPragmasAndConcurrentSave checks that every pooled connection gets
// the per-connection pragmas and that a capture save can run while a large UI
// session read is in progress.
func TestPooledPragmasAndConcurrentSave(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pool.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	assertPooledPragmas(t, st.DB(), ctx)

	// A highly compressible body keeps the database small while making the
	// tree read spend real time decompressing it.
	body := bytes.Repeat([]byte("x"), 512<<10)
	const requestCount = 200
	started := time.Now().UTC()
	for i := 0; i < requestCount; i++ {
		rec := &store.RequestRecord{
			SessionKey:   "large-read",
			StartedAt:    started.Add(time.Duration(i) * time.Millisecond),
			Method:       "POST",
			Endpoint:     "/openrouter/v1/chat/completions",
			Provider:     "openrouter",
			Model:        "model",
			StatusCode:   200,
			RequestBody:  body,
			ResponseBody: body,
			Parser:       "openai_chat",
		}
		if _, err := st.SaveRequest(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	var sessionID int64
	if err := st.DB().QueryRowContext(ctx,
		`SELECT id FROM sessions WHERE session_key = ?`, "large-read").Scan(&sessionID); err != nil {
		t.Fatal(err)
	}

	readStarted := make(chan struct{})
	readDone := make(chan error, 1)
	readStart := time.Now()
	go func() {
		close(readStarted)
		_, err := st.ListRequestsForTree(context.Background(), sessionID)
		readDone <- err
	}()
	<-readStarted
	time.Sleep(5 * time.Millisecond)

	small := func() *store.RequestRecord {
		now := time.Now().UTC()
		return &store.RequestRecord{
			SessionKey:   "concurrent",
			StartedAt:    now,
			CompletedAt:  &now,
			Method:       "POST",
			Endpoint:     "/openrouter/v1/chat/completions",
			Provider:     "openrouter",
			Model:        "model",
			StatusCode:   200,
			RequestBody:  []byte(`{"messages":[]}`),
			ResponseBody: []byte(`{"choices":[]}`),
			Parser:       "openai_chat",
		}
	}

	deadline := time.Now().Add(10 * time.Second)
	overlapping := 0
	for time.Now().Before(deadline) {
		select {
		case err := <-readDone:
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("large read took %v; %d saves completed while it ran", time.Since(readStart), overlapping)
			if overlapping == 0 {
				t.Fatal("no save completed while the large read was running")
			}
			return
		default:
		}
		saveStart := time.Now()
		if _, err := st.SaveRequest(ctx, small()); err != nil {
			t.Fatal(err)
		}
		saveDur := time.Since(saveStart)
		select {
		case err := <-readDone:
			if err != nil {
				t.Fatal(err)
			}
			readDur := time.Since(readStart)
			// If the read finished during this save, the save only overlapped
			// when it was much shorter than the read. A single connection makes
			// the save wait for the read, so saveDur is close to readDur.
			if saveDur < readDur/2 {
				overlapping++
			}
			t.Logf("large read took %v; %d saves completed while it ran (last save %v)", readDur, overlapping, saveDur)
			if overlapping == 0 {
				t.Fatalf("save waited for the large read: save %v, read %v", saveDur, readDur)
			}
			return
		default:
			// The read is still running after the save returned.
			overlapping++
		}
	}
	t.Fatal("timed out waiting for the large read")
}

func assertPooledPragmas(t *testing.T, db *sql.DB, ctx context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conns := make([]*sql.Conn, 0, store.MaxOpenConns)
	for i := 0; i < store.MaxOpenConns; i++ {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("acquire connection %d of %d: %v", i+1, store.MaxOpenConns, err)
		}
		conns = append(conns, c)
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i, c := range conns {
		var foreignKeys, busyTimeout int
		if err := c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 1 {
			t.Errorf("connection %d: foreign_keys=%d, want 1", i, foreignKeys)
		}
		if busyTimeout != 5000 {
			t.Errorf("connection %d: busy_timeout=%d, want 5000", i, busyTimeout)
		}
	}
}
