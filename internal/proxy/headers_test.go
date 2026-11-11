// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package proxy

import (
	"net/http"
	"reflect"
	"testing"
)

func TestCopyEndToEndHeadersDropsConnectionNominated(t *testing.T) {
	src := http.Header{}
	src.Set("Content-Type", "application/json")
	src.Add("X-Repeat", "one")
	src.Add("X-Repeat", "two")
	src.Set("Connection", "X-Custom-Hop, keep-alive")
	src.Set("X-Custom-Hop", "secret")
	src.Set("Keep-Alive", "timeout=5")
	src.Set("Transfer-Encoding", "chunked")
	src.Set("Upgrade", "h2c")
	src.Set("Proxy-Connection", "keep-alive")
	src.Set("Te", "trailers")
	src.Set("Trailer", "X-Trailer")

	dst := http.Header{}
	copyEndToEndHeaders(dst, src)

	if got := dst.Values("X-Repeat"); !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatalf("repeated end-to-end values not preserved: %#v", got)
	}
	if got := dst.Get("Content-Type"); got != "application/json" {
		t.Fatalf("end-to-end header lost: %q", got)
	}
	for _, name := range []string{
		"Connection", "X-Custom-Hop", "Keep-Alive", "Transfer-Encoding",
		"Upgrade", "Proxy-Connection", "Te", "Trailer",
	} {
		if got := dst.Values(name); len(got) != 0 {
			t.Fatalf("hop-by-hop header %q was forwarded: %#v", name, got)
		}
	}
}

func TestCopyEndToEndHeadersConnectionCasingAndMultipleLines(t *testing.T) {
	src := http.Header{}
	src.Set("X-Keep", "yes")
	src.Add("Connection", "x-first-hop")
	src.Add("connection", "X-SECOND-HOP, x-third-hop")
	src.Set("X-First-Hop", "1")
	src.Set("x-second-hop", "2")
	src.Set("X-Third-Hop", "3")

	dst := http.Header{}
	copyEndToEndHeaders(dst, src)

	if got := dst.Get("X-Keep"); got != "yes" {
		t.Fatalf("end-to-end header lost: %q", got)
	}
	for _, name := range []string{"Connection", "X-First-Hop", "X-Second-Hop", "X-Third-Hop"} {
		if got := dst.Values(name); len(got) != 0 {
			t.Fatalf("connection-nominated header %q was forwarded: %#v", name, got)
		}
	}
}

func TestCopyEndToEndHeadersWithoutConnectionHeader(t *testing.T) {
	src := http.Header{}
	src.Set("Authorization", "Bearer token")
	src.Set("X-Trace", "abc")

	dst := http.Header{}
	copyEndToEndHeaders(dst, src)

	if got := dst.Get("Authorization"); got != "Bearer token" {
		t.Fatalf("authorization must be forwarded unchanged: %q", got)
	}
	if got := dst.Get("X-Trace"); got != "abc" {
		t.Fatalf("ordinary header lost: %q", got)
	}
}

func TestConnectionNominatedHeadersEmpty(t *testing.T) {
	if got := connectionNominatedHeaders(http.Header{}); got != nil {
		t.Fatalf("expected no nominated headers, got %#v", got)
	}
	src := http.Header{}
	src.Set("Connection", " , ")
	if got := connectionNominatedHeaders(src); got != nil {
		t.Fatalf("expected blank names to be ignored, got %#v", got)
	}
}
