// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompactBodies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	now := formatTime(time.Now().UTC())

	pretty := []byte("{\n  \"model\": \"m\",\n  \"messages\": [{\"role\": \"user\", \"content\": \"" +
		strings.Repeat("x", 2000) + "\"}]\n}")
	prefixed := append(append([]byte{}, storagePrefix...), []byte("payload")...)
	insert := func(body []byte) int64 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO requests
			(started_at, method, local_path, upstream_url, status_code, parser, request_body, response_body, created_at)
			VALUES (?, 'POST', '/x', 'http://up/x', 200, '', ?, ?, ?)`,
			now, body, body, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	largeID := insert(pretty)
	prefixedID := insert(prefixed)

	changed, err := s.CompactBodies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 2 {
		t.Fatalf("expected 2 rows changed, got %d", changed)
	}

	var stored []byte
	if err := s.db.QueryRowContext(ctx, `SELECT request_body FROM requests WHERE id = ?`, largeID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(stored, storageMagic) {
		t.Fatalf("large body was not compressed: %x", stored[:len(storageMagic)])
	}
	detail, err := s.GetRequest(ctx, largeID)
	if err != nil {
		t.Fatal(err)
	}
	var minified bytes.Buffer
	if err := json.Compact(&minified, pretty); err != nil {
		t.Fatal(err)
	}
	if detail.RequestBody != minified.String() {
		t.Fatalf("large body not restored")
	}

	detail, err = s.GetRequest(ctx, prefixedID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.RequestBody != string(prefixed) {
		t.Fatalf("prefixed body not restored: %q", detail.RequestBody)
	}

	changed, err = s.CompactBodies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatalf("second run should be a no-op, changed %d", changed)
	}
}
