// Index helpers for jumping to a specific message row under virtualization,
// where off-screen rows are not in the DOM (so querySelector can't find them).
// Callers map a msgid or message id to a row index, then scroll the virtualizer
// to that index.

/** Map each non-empty msgid to its index in the row list. */
export function indexByMsgid(rows: ReadonlyArray<{ msgid?: string }>): Map<string, number> {
  const m = new Map<string, number>()
  rows.forEach((row, i) => {
    if (row.msgid) m.set(row.msgid, i)
  })
  return m
}

/** Map each row's message id to its index in the row list. */
export function indexById(rows: ReadonlyArray<{ id: number }>): Map<number, number> {
  const m = new Map<number, number>()
  rows.forEach((row, i) => m.set(row.id, i))
  return m
}
