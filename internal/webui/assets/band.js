// SPDX-License-Identifier: AGPL-3.0-or-later

// A band is a run of rows that a drag can reorder.
//
// A list view sorts by the day, then the priority, then the order key. That
// sort is the one `internal/store/store.go` writes into SQL and the one the
// phone writes into Room, so all three clients draw a view the same way. A
// manual order can therefore only act on the last key, and only inside a band:
// the rows that agree on the two keys above it, one day and one priority.
//
// A band always holds rows that stand next to each other, because the sort
// puts them there. A drop renumbers the band and nothing else.
//
// The band arithmetic is here, with no state and no document, so a test can
// read it. `app.js` holds the drag, the keys and the writes.

// keyAt makes the order key of one position. The keys are ten apart, in the
// fixed-width form that `internal/todoist/import.go` also writes, so a list
// that came from an import and a list a person dragged carry one shape of key.
//
// A drop renumbers the whole band instead of finding a key between two
// neighbours. A band holds a few rows, and one command per row is the shape
// that D-008 asks for.
export function keyAt(i) {
  return 'm' + String((i + 1) * 10).padStart(6, '0');
}

// after makes the key one step past a key of the form keyAt writes. A new row
// needs it to land at the end of a band somebody arranged, rather than at the
// top of it: the key that all three clients write by default is the literal
// `m`, and that sorts before every key of this form.
//
// A key of another shape, and a key at the end of the range, both give an
// empty string. The caller then keeps the default key, rather than write a key
// that sorts in a place nobody asked for.
export function after(key) {
  if (!/^m[0-9]{6}$/.test(key)) return '';
  const n = Number(key.slice(1)) + 10;
  if (n > 999999) return '';
  return 'm' + String(n).padStart(6, '0');
}

// bandAt finds the band around one index of the list.
//
// `bandOf` names the band of one row. Two rows are in one band when the two
// names are equal. The caller owns the rule, because the day of a row is the
// caller's business.
//
// It returns the first index, the last index and the rows, or null when the
// index is outside the list.
export function bandAt(rows, at, bandOf) {
  if (at < 0 || at >= rows.length) return null;
  const name = bandOf(rows[at]);
  let from = at;
  let to = at;
  while (from > 0 && bandOf(rows[from - 1]) === name) from--;
  while (to < rows.length - 1 && bandOf(rows[to + 1]) === name) to++;
  return { from, to, rows: rows.slice(from, to + 1) };
}

// reorder moves one row of a band to one position.
//
// Both indexes count from the start of the band. `from` names the row that
// moves. `to` names the row that the moved row must stand before, so a `to` of
// the length of the band means the end of it. A drop on the lower half of a
// row gives the index after that row, which is how a pointer says "below this
// one".
//
// It returns the new order of the band.
export function reorder(rows, from, to) {
  const out = rows.slice();
  if (from < 0 || from >= out.length) return out;
  const [moved] = out.splice(from, 1);
  const at = to > from ? to - 1 : to;
  out.splice(Math.min(Math.max(at, 0), out.length), 0, moved);
  return out;
}

// rekey reads an order of rows and returns the rows whose key must change,
// each with its new key. A row that already carries the right key is not in
// the answer, so a second move inside one band costs fewer commands than the
// first one.
export function rekey(rows) {
  const out = [];
  rows.forEach((r, i) => {
    const key = keyAt(i);
    if ((r.order_key || 'm') === key) return;
    out.push({ id: r.id, order_key: key });
  });
  return out;
}
