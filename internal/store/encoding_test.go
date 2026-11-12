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

	"github.com/klauspost/compress/zstd"
)

func zstdCompressed(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestEncodeBodyRoundTrip(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"` + strings.Repeat("hello ", 200) + `"}]}`)
	encoded := encodeBody(body)
	if !bytes.HasPrefix(encoded, storageMagic) {
		t.Fatalf("expected storage magic, got %x", encoded[:len(storageMagic)])
	}
	if len(encoded) >= len(body) {
		t.Fatalf("expected compression, got %d >= %d", len(encoded), len(body))
	}
	if got := decodeBody(encoded); !bytes.Equal(got, body) {
		t.Fatalf("round trip mismatch: %q", got)
	}
}

func TestEncodeBodyMinifiesJSON(t *testing.T) {
	pretty := []byte("{\n  \"a\": 1,\n  \"b\": [1, 2]\n}")
	encoded := encodeBody(pretty)
	if got := decodeBody(encoded); string(got) != `{"a":1,"b":[1,2]}` {
		t.Fatalf("unexpected minified body: %q", got)
	}
}

func TestEncodeBodyLeavesSmallAndNonJSONAlone(t *testing.T) {
	small := []byte(`{"a":1}`)
	if got := encodeBody(small); !bytes.Equal(got, small) {
		t.Fatalf("small body should be stored verbatim: %q", got)
	}
	text := []byte(strings.Repeat("not json ", 100))
	if got := decodeBody(encodeBody(text)); !bytes.Equal(got, text) {
		t.Fatalf("non-JSON body round trip changed: %q", got)
	}
}

func TestDecodeBodyLeavesUncompressedAlone(t *testing.T) {
	raw := []byte(`{"old":true}`)
	if got := decodeBody(raw); !bytes.Equal(got, raw) {
		t.Fatalf("uncompressed body changed: %q", got)
	}
}

func TestEncodeBodyEscapesStoragePrefix(t *testing.T) {
	for _, prefix := range [][]byte{
		storagePrefix,
		storageMagic,
		escapeMagic,
		append(append([]byte{}, storagePrefix...), 'x'),
	} {
		body := append(append([]byte{}, prefix...), []byte("payload")...)
		encoded := encodeBody(body)
		if !bytes.HasPrefix(encoded, escapeMagic) {
			t.Fatalf("body starting with %x was not escaped: %x", prefix, encoded)
		}
		if got := decodeBody(encoded); !bytes.Equal(got, body) {
			t.Fatalf("escaped round trip mismatch: %q", got)
		}
	}
}

func TestEncodeBodyCompressesPrefixedLargeBody(t *testing.T) {
	body := append(append([]byte{}, storageMagic...), bytes.Repeat([]byte("x"), 2000)...)
	encoded := encodeBody(body)
	if !bytes.HasPrefix(encoded, storageMagic) {
		t.Fatalf("expected compression, got %x", encoded[:len(storageMagic)])
	}
	if got := decodeBody(encoded); !bytes.Equal(got, body) {
		t.Fatalf("round trip mismatch")
	}
}

func TestEncodeBodyEscapedSuffixStillDecodes(t *testing.T) {
	// The payload after the escape marker must not itself be decoded.
	body := append(append([]byte{}, escapeMagic...), escapeMagic...)
	if got := decodeBody(encodeBody(body)); !bytes.Equal(got, body) {
		t.Fatalf("nested escape mismatch: %q", got)
	}
}

func TestDecodeBodyFallsBackOnBadMagic(t *testing.T) {
	raw := append(append([]byte{}, storageMagic...), []byte("not a frame")...)
	if got := decodeBody(raw); !bytes.Equal(got, raw) {
		t.Fatalf("bad frame should fall back to raw: %q", got)
	}

	// A payload that is already zstd-compressed on the wire must not be
	// mistaken for a storage-compressed blob.
	wire := zstdCompressed(t, []byte(`{"a":1}`))
	if got := decodeBody(wire); !bytes.Equal(got, wire) {
		t.Fatalf("wire-compressed body was decoded as storage: %q", got)
	}
}

func TestStoreCompressesBodiesAndReadsOldRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"` + strings.Repeat("x", 2000) + `"}]}`)
	rec := &RequestRecord{
		SessionKey: "s1", Conversation: true, StartedAt: now, Method: "POST", Endpoint: "/x",
		Provider: "p", StatusCode: 200,
		Parser: "openai_chat", RequestBody: body, ResponseBody: body,
	}
	id, err := s.SaveRequest(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}

	var stored []byte
	if err := s.db.QueryRowContext(ctx, `SELECT request_body FROM requests WHERE id = ?`, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(stored, storageMagic) {
		t.Fatalf("expected compressed row, got %x", stored[:len(storageMagic)])
	}

	detail, err := s.GetRequest(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.RequestBody != string(body) || detail.ResponseBody != string(body) {
		t.Fatalf("detail bodies not restored")
	}

	tree, err := s.ListRequestsForTree(ctx, *detail.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || string(tree[0].RequestBody) != string(body) {
		t.Fatalf("tree bodies not restored: %#v", tree)
	}

	// A row written before compression existed must still read back unchanged.
	old := []byte(`{"old":true}`)
	res, err := s.db.ExecContext(ctx, `INSERT INTO requests
		(started_at, method, endpoint, status_code, parser, request_body, response_body)
		VALUES (?, 'POST', '/old', 200, '', ?, ?)`,
		formatTime(now), old, old)
	if err != nil {
		t.Fatal(err)
	}
	oldID, _ := res.LastInsertId()
	oldDetail, err := s.GetRequest(ctx, oldID)
	if err != nil {
		t.Fatal(err)
	}
	if oldDetail.RequestBody != string(old) || oldDetail.ResponseBody != string(old) {
		t.Fatalf("old row changed: %q", oldDetail.RequestBody)
	}
}

func TestEncodeBodyKeepsJSONSemantics(t *testing.T) {
	body := []byte(`{"n":1.50,"s":"a\"b","arr":[true,null]}`)
	encoded := encodeBody(body)
	decoded := decodeBody(encoded)
	var want, got any
	if err := json.Unmarshal(body, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(decoded, &got); err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(want, got) {
		t.Fatalf("semantics changed: %s", decoded)
	}
}

func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(ab, bb)
}
