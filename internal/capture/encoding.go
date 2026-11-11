// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package capture

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"strings"

	"github.com/andybalholm/brotli"
)

// Decode decodes a complete body according to its Content-Encoding header.
// Unknown or empty encodings are returned unchanged.
func Decode(data []byte, contentEncoding string) ([]byte, error) {
	encodings := splitEncodings(contentEncoding)
	for i := len(encodings) - 1; i >= 0; i-- {
		switch encodings[i] {
		case "", "identity":
			continue
		case "gzip", "x-gzip":
			r, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				return nil, fmt.Errorf("gzip: %w", err)
			}
			out, err := io.ReadAll(r)
			closeErr := r.Close()
			if err != nil {
				return nil, fmt.Errorf("gzip: %w", err)
			}
			if closeErr != nil {
				return nil, fmt.Errorf("gzip: %w", closeErr)
			}
			data = out
		case "deflate":
			out, err := decodeDeflate(data)
			if err != nil {
				return nil, err
			}
			data = out
		case "br":
			out, err := io.ReadAll(brotli.NewReader(bytes.NewReader(data)))
			if err != nil {
				return nil, fmt.Errorf("brotli: %w", err)
			}
			data = out
		default:
			// Preserve unknown encodings rather than corrupting the payload.
			return data, nil
		}
	}
	return data, nil
}

func decodeDeflate(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err == nil {
		out, readErr := io.ReadAll(r)
		closeErr := r.Close()
		if readErr != nil {
			return nil, fmt.Errorf("deflate: %w", readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("deflate: %w", closeErr)
		}
		return out, nil
	}
	// Some servers send raw DEFLATE rather than zlib-wrapped DEFLATE.
	fr := flate.NewReader(bytes.NewReader(data))
	out, readErr := io.ReadAll(fr)
	closeErr := fr.Close()
	if readErr != nil {
		return nil, fmt.Errorf("deflate: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("deflate: %w", closeErr)
	}
	return out, nil
}

// Decoder wraps r with the decoding implied by contentEncoding. The returned
// reader must be closed.
func Decoder(r io.Reader, contentEncoding string) (io.ReadCloser, error) {
	encodings := splitEncodings(contentEncoding)
	var rc io.ReadCloser = io.NopCloser(r)
	for i := len(encodings) - 1; i >= 0; i-- {
		switch encodings[i] {
		case "", "identity":
			continue
		case "gzip", "x-gzip":
			gr, err := gzip.NewReader(rc)
			if err != nil {
				_ = rc.Close()
				return nil, fmt.Errorf("gzip: %w", err)
			}
			rc = &multiCloser{Reader: gr, closers: []io.Closer{gr, rc}}
		case "deflate":
			zr, err := zlib.NewReader(rc)
			if err != nil {
				_ = rc.Close()
				return nil, fmt.Errorf("deflate: %w", err)
			}
			rc = &multiCloser{Reader: zr, closers: []io.Closer{zr, rc}}
		case "br":
			rc = &multiCloser{Reader: brotli.NewReader(rc), closers: []io.Closer{rc}}
		default:
			// Unknown encoding: expose the bytes as-is.
			return rc, nil
		}
	}
	return rc, nil
}

type multiCloser struct {
	io.Reader
	closers []io.Closer
}

func (m *multiCloser) Close() error {
	var first error
	for _, c := range m.closers {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func splitEncodings(header string) []string {
	if strings.TrimSpace(header) == "" {
		return nil
	}
	parts := strings.Split(header, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
