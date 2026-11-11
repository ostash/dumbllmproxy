// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package capture

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"testing"

	"github.com/andybalholm/brotli"
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
