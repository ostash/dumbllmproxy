// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"bytes"
	"context"
)

// CompactBodies rewrites every stored body using the current storage encoding
// and returns the number of rows changed. It is a one-off upgrade helper for
// databases written before compression existed. Bodies that already carry a
// storage marker are left alone, so running it twice is safe.
func (s *Store) CompactBodies(ctx context.Context) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT id, request_body, response_body, raw_response_body FROM requests ORDER BY id`)
	if err != nil {
		return 0, err
	}
	type storedBodies struct {
		id                     int64
		request, response, raw []byte
	}
	var bodies []storedBodies
	for rows.Next() {
		var b storedBodies
		if err := rows.Scan(&b.id, &b.request, &b.response, &b.raw); err != nil {
			rows.Close()
			return 0, err
		}
		bodies = append(bodies, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	changed := 0
	for _, b := range bodies {
		request := compactStoredBody(b.request)
		response := compactStoredBody(b.response)
		raw := compactStoredBody(b.raw)
		if bytes.Equal(request, b.request) && bytes.Equal(response, b.response) && bytes.Equal(raw, b.raw) {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE requests SET request_body = ?, response_body = ?, raw_response_body = ? WHERE id = ?`,
			request, response, raw, b.id); err != nil {
			return 0, err
		}
		changed++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return changed, nil
}

func compactStoredBody(body []byte) []byte {
	if bytes.HasPrefix(body, storageMagic) || bytes.HasPrefix(body, escapeMagic) {
		return body
	}
	return encodeBody(body)
}

// Vacuum rebuilds the database file to reclaim space freed by CompactBodies.
func (s *Store) Vacuum(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `VACUUM`)
	return err
}
