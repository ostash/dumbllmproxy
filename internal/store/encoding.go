// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"bytes"
	"encoding/json"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// Stored blobs are self-describing:
//
//	storageMagic = 00 44 4C 50 01  zstd-compressed (last byte is the codec)
//	escapeMagic  = 00 44 4C 50 00  verbatim payload that would otherwise look
//	                              like a marker; the next bytes are the payload
//
// Both are deliberately distinct from any wire format, so a payload that was
// already zstd-compressed in transit is never mistaken for a storage blob. The
// escape keeps the format unambiguous even for a small payload that happens to
// start with 00 44 4C 50. A blob with neither prefix is verbatim, which keeps
// rows written before compression existed readable without a migration.
var (
	storagePrefix = []byte{0x00, 'D', 'L', 'P'}
	storageMagic  = []byte{0x00, 'D', 'L', 'P', storageCodecZstd}
	escapeMagic   = []byte{0x00, 'D', 'L', 'P', 0x00}
)

const storageCodecZstd = 0x01

// minCompressSize avoids inflating tiny bodies with a frame header.
const minCompressSize = 256

var (
	encoderPool = sync.Pool{New: func() any {
		enc, _ := zstd.NewWriter(nil)
		return enc
	}}
	decoderPool = sync.Pool{New: func() any {
		dec, _ := zstd.NewReader(nil)
		return dec
	}}
)

// encodeBody prepares a body for storage: JSON is minified, then the result is
// zstd-compressed when that actually saves space. Bodies that are not JSON, are
// too small, or do not compress are stored verbatim, except that a body which
// starts with the storage prefix is escaped so it cannot be mistaken for a
// marker.
func encodeBody(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	if compacted, ok := compactJSON(body); ok {
		body = compacted
	}
	if len(body) >= minCompressSize {
		if compressed := compressBody(body); len(compressed) < len(body) {
			return compressed
		}
	}
	if bytes.HasPrefix(body, storagePrefix) {
		out := make([]byte, 0, len(escapeMagic)+len(body))
		out = append(out, escapeMagic...)
		out = append(out, body...)
		return out
	}
	return body
}

func compressBody(body []byte) []byte {
	enc := encoderPool.Get().(*zstd.Encoder)
	defer encoderPool.Put(enc)
	out := make([]byte, len(storageMagic))
	copy(out, storageMagic)
	return enc.EncodeAll(body, out)
}

// decodeBody reverses encodeBody. Escaped bodies are unescaped, compressed
// bodies are decompressed, and everything else (including rows written before
// compression existed) is returned unchanged.
func decodeBody(body []byte) []byte {
	if bytes.HasPrefix(body, escapeMagic) {
		return body[len(escapeMagic):]
	}
	if !bytes.HasPrefix(body, storageMagic) {
		return body
	}
	dec := decoderPool.Get().(*zstd.Decoder)
	defer decoderPool.Put(dec)
	out, err := dec.DecodeAll(body[len(storageMagic):], nil)
	if err != nil {
		// A body that merely starts with the magic bytes is not compressed.
		return body
	}
	return out
}

// compactJSON removes insignificant whitespace without changing key order,
// number formatting or string contents.
func compactJSON(body []byte) ([]byte, bool) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, body); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}
