// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

type Props = {
  reported: number;
  total: number;
  label: string;
};

// PartialChip marks aggregated values that are missing data from some of the
// requests they summarize. It renders nothing when every request reported.
export function PartialChip({ reported, total, label }: Props) {
  if (reported >= total) return null;
  return (
    <span className="chip warn small-chip" title={`${reported} of ${total} requests reported ${label}`}>
      partial
    </span>
  );
}

export function hasNoData(reported: number): boolean {
  return reported === 0;
}
