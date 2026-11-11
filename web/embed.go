// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package web

import "embed"

// Dist contains the built frontend. Run "make frontend" (or "npm run build" in
// web/) to regenerate it. Only a placeholder is committed, so a fresh checkout
// still compiles; the UI reports that the frontend has not been built until it
// is.
//
//go:embed all:dist
var Dist embed.FS
