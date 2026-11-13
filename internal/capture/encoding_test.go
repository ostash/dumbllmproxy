// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package capture

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

func gzipped(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodeGzip(t *testing.T) {
	input := []byte("hello world")
	out, err := Decode(gzipped(t, input), "gzip")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, input) {
		t.Fatalf("unexpected decoded data: %q", out)
	}
}

func TestDecodeDeflateZlib(t *testing.T) {
	input := []byte("deflate data")
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write(input)
	_ = w.Close()
	out, err := Decode(buf.Bytes(), "deflate")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, input) {
		t.Fatalf("unexpected decoded data: %q", out)
	}
}

func TestDecodeBrotli(t *testing.T) {
	input := []byte("brotli data")
	var buf bytes.Buffer
	w := brotli.NewWriter(&buf)
	_, _ = w.Write(input)
	_ = w.Close()
	out, err := Decode(buf.Bytes(), "br")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, input) {
		t.Fatalf("unexpected decoded data: %q", out)
	}
}

func TestDecodeIdentityAndUnknown(t *testing.T) {
	input := []byte("plain")
	out, err := Decode(input, "")
	if err != nil || !bytes.Equal(out, input) {
		t.Fatalf("identity failed: %q %v", out, err)
	}
	out, err = Decode(input, "x-custom")
	if err != nil || !bytes.Equal(out, input) {
		t.Fatalf("unknown encoding should pass through: %q %v", out, err)
	}
}

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

func TestDecodeZstd(t *testing.T) {
	input := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	out, err := Decode(zstdCompressed(t, input), "zstd")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, input) {
		t.Fatalf("unexpected decoded data: %q", out)
	}
}

func TestDecodeNestedEncodings(t *testing.T) {
	input := []byte("nested body")
	// Content-Encoding lists encodings in the order they were applied.
	outer := zstdCompressed(t, gzipped(t, input))
	out, err := Decode(outer, "gzip, zstd")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, input) {
		t.Fatalf("unexpected decoded data: %q", out)
	}
}

func TestDecodeEmptyBody(t *testing.T) {
	out, err := Decode(nil, "gzip")
	if err != nil {
		t.Fatalf("empty body should not fail: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("unexpected data: %q", out)
	}
}
