# Backlog

Everything here is a known gap, left on purpose. Each entry says what is missing
and why it waits. A defect that loses data does not belong here. It belongs in a
fix.

---

## A project name that holds a filter operator reaches no client

**Found:** 2026-08-27 · **Where:** `filter/filter.go`, every client

A view of a project is the query `#Name`. The grammar reads `&`, `|`, `,`, `!`,
`(` and `)` as operators, so a project named `Home & Garden` compiles to `#Home`
AND the word `Garden`, and the view is empty or wrong. The browser builds the
same string and has the same gap.

**Why it waits.** The fix is a quoted name in the grammar, `#"Home & Garden"`.
That is three parsers, not one: the Go compiler, the JavaScript evaluator in the
web app, and the corpus that holds them together. The phone would drift from the
browser until all three land. No project in the account carries such a name
today.

**What closes it.** A quoted form in the lexer, the same form in
`internal/webui/assets/app.js`, and cases in `filter/filter_test.go`. A view of a
project could then also carry the project id instead of the name, which is exact.

---

## The browser reads a subset of the filter grammar

**Found:** before 2026-08-27 · **Where:** `internal/webui/assets/app.js`

The browser evaluates the filter in JavaScript over the rows in
`localStorage`. It knows fewer terms than the server, and a term it does not know
becomes a title search, so a view can quietly show the wrong rows.
`docs/USAGE.md` holds the table of what it knows.

The phone no longer has this gap. It calls the shared Go compiler through the
gomobile binding, so it reads every term the server reads.

**Why it waits.** The web app needs the compiler, not a second evaluator. Two
ways in: the shared packages compiled to WebAssembly, or a `POST /v1/query` that
answers with ids. Both are bigger than a filter fix, and the second one only
works online.

---

## The phone refuses `created:`

**Found:** 2026-08-27 · **Where:** `filter/schema.go`, `RoomSchema`

The Room database keeps no creation date, so the term cannot be answered there.
The compiler fails with a sentence that says so. It does not name a column that
the database has not got.

**Why it waits.** The column is one field on `TaskEntity` and one line in the
sync mapping, and no view needs it yet. A schema change costs one full pull, so
it travels with the next change that needs one.

---

## The phone keeps no order inside a day

A view sorts by the day, then the priority, then the order key. The server holds
a fractional order key per project, and the phone never edits it, so a task
cannot be dragged into place on the phone. Nothing is lost, and the browser
cannot reorder either.
