// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteRequest(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC()

	save := func(sessionKey string, offset int) int64 {
		id, err := s.SaveRequest(ctx, &RequestRecord{
			SessionKey:  sessionKey,
			StartedAt:   now.Add(time.Duration(offset) * time.Second),
			Method:      "POST",
			Endpoint:    "/x",
			Provider:    "p",
			StatusCode:  200,
			RequestBody: []byte(`{}`),
			Parser:      "openai_chat",
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	first := save("session-a", 0)
	second := save("session-a", 1)
	other := save("session-b", 2)

	var sessionID int64
	if err := s.db.QueryRowContext(ctx, `SELECT session_id FROM requests WHERE id = ?`, first).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}

	deleted, err := s.DeleteRequest(ctx, first)
	if err != nil || !deleted {
		t.Fatalf("delete first: deleted=%v err=%v", deleted, err)
	}
	var sessions int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatalf("session removed while another request remains: %d", sessions)
	}
	reqs, err := s.ListRequests(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 || reqs[0].ID != second {
		t.Fatalf("unexpected remaining requests: %#v", reqs)
	}

	deleted, err = s.DeleteRequest(ctx, second)
	if err != nil || !deleted {
		t.Fatalf("delete second: deleted=%v err=%v", deleted, err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("empty session was not removed: %d", sessions)
	}

	deleted, err = s.DeleteRequest(ctx, second)
	if err != nil || deleted {
		t.Fatalf("delete missing: deleted=%v err=%v", deleted, err)
	}

	deleted, err = s.DeleteRequest(ctx, other)
	if err != nil || !deleted {
		t.Fatalf("delete other: deleted=%v err=%v", deleted, err)
	}
	unassigned := save("", 3)
	deleted, err = s.DeleteRequest(ctx, unassigned)
	if err != nil || !deleted {
		t.Fatalf("delete unassigned: deleted=%v err=%v", deleted, err)
	}
	var remaining int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM requests`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("unexpected remaining requests: %d", remaining)
	}
	var sessionsLeft int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&sessionsLeft); err != nil {
		t.Fatal(err)
	}
	if sessionsLeft != 0 {
		t.Fatalf("unexpected remaining sessions: %d", sessionsLeft)
	}
}

func TestDeleteRequests(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC()

	save := func(sessionKey string, offset int) int64 {
		id, err := s.SaveRequest(ctx, &RequestRecord{
			SessionKey:  sessionKey,
			StartedAt:   now.Add(time.Duration(offset) * time.Second),
			Method:      "POST",
			Endpoint:    "/x",
			Provider:    "p",
			StatusCode:  200,
			RequestBody: []byte(`{}`),
			Parser:      "openai_chat",
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	a1 := save("session-a", 0)
	a2 := save("session-a", 1)
	b := save("session-b", 2)
	unassigned := save("", 3)

	deleted, err := s.DeleteRequests(ctx, []int64{a1, a1, 999999, a2})
	if err != nil || deleted != 2 {
		t.Fatalf("delete session-a batch: deleted=%v err=%v", deleted, err)
	}
	countSession := func(key string) int {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE session_key = ?`, key).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if countSession("session-a") != 0 {
		t.Fatal("session-a was not removed")
	}
	if countSession("session-b") != 1 {
		t.Fatal("session-b was removed unexpectedly")
	}
	var remaining int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM requests`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 2 {
		t.Fatalf("remaining requests = %d, want 2", remaining)
	}

	deleted, err = s.DeleteRequests(ctx, []int64{b, unassigned})
	if err != nil || deleted != 2 {
		t.Fatalf("delete remaining: deleted=%v err=%v", deleted, err)
	}
	var sessions int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("remaining sessions = %d, want 0", sessions)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM requests`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("remaining requests = %d, want 0", remaining)
	}
}
