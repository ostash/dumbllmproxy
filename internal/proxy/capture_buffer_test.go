// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package proxy

import "testing"

func TestCaptureBufferTruncates(t *testing.T) {
	b := captureBuffer{limit: 5}
	if _, err := b.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("defg")); err != nil {
		t.Fatal(err)
	}
	if got := string(b.Bytes()); got != "abcde" {
		t.Fatalf("captured %q, want %q", got, "abcde")
	}
	if !b.truncated {
		t.Fatal("expected truncation")
	}
	if _, err := b.Write([]byte("hij")); err != nil {
		t.Fatal(err)
	}
	if got := string(b.Bytes()); got != "abcde" {
		t.Fatalf("captured %q after extra write, want %q", got, "abcde")
	}
}
