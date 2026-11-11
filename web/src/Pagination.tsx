// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

import { useEffect, useState } from "react";
import { PAGE_SIZES, type PageSize } from "./filters";

type Props = {
  page: number;
  pageSize: PageSize;
  total: number;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: PageSize) => void;
};

export function Pagination({ page, pageSize, total, onPageChange, onPageSizeChange }: Props) {
  const pages = Math.max(1, Math.ceil(total / pageSize));
  const start = total === 0 ? 0 : page * pageSize + 1;
  const end = Math.min(total, (page + 1) * pageSize);
  const [jump, setJump] = useState(String(page + 1));

  useEffect(() => {
    setJump(String(page + 1));
  }, [page]);

  function commitJump() {
    const value = Number(jump);
    if (!Number.isFinite(value)) {
      setJump(String(page + 1));
      return;
    }
    const target = Math.min(pages - 1, Math.max(0, Math.floor(value) - 1));
    if (target !== page) onPageChange(target);
    setJump(String(target + 1));
  }

  return (
    <div className="pagination">
      <span className="muted small">Showing {start}–{end} of {total}</span>
      <label className="muted small pagination-size">
        Rows
        <select
          value={pageSize}
          onChange={(e) => onPageSizeChange(Number(e.target.value) as PageSize)}
        >
          {PAGE_SIZES.map((size) => (
            <option key={size} value={size}>{size}</option>
          ))}
        </select>
      </label>
      <div className="pagination-buttons">
        <button className="secondary" onClick={() => onPageChange(0)} disabled={page <= 0}>
          First
        </button>
        <button className="secondary" onClick={() => onPageChange(page - 1)} disabled={page <= 0}>
          Previous
        </button>
        <label className="muted small page-jump-label">
          Page
          <input
            className="page-jump"
            type="number"
            min={1}
            max={pages}
            value={jump}
            onChange={(e) => setJump(e.target.value)}
            onBlur={commitJump}
            onKeyDown={(e) => {
              if (e.key === "Enter") commitJump();
            }}
            aria-label="Page number"
          />
          / {pages}
        </label>
        <button className="secondary" onClick={() => onPageChange(page + 1)} disabled={page + 1 >= pages}>
          Next
        </button>
        <button className="secondary" onClick={() => onPageChange(pages - 1)} disabled={page + 1 >= pages}>
          Last
        </button>
      </div>
    </div>
  );
}
