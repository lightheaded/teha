# Backlog

Everything the build knowingly leaves unfinished, with the reason. §9 of
[PLAN.md](PLAN.md) asks for this file, so that a gap is a decision on a page and
not a surprise in a year.

A row leaves this file when a test covers it.

## Sync and the store

- **Three clients still write the literal `m` into `order_key`.** The
  fractional index now exists as the `order` package with a property test, and
  D-013 in [DECISIONS.md](DECISIONS.md) records why. No client calls it: the
  web app, the Android repository and the store all write `m`, and the importer
  writes a fixed-width number of the Todoist child order. Until a client adopts
  the package, a list falls back to its secondary sort keys and a drag cannot be
  saved. Adopting it is client work in three places, so it waits for the next
  client milestone.

- **The fractional index only halves a gap, so a key grows.** About one
  character for every six insertions into the same point: 2 000 insertions into
  one gap give a key of 401 characters, and 2 000 appends at the end give 334.
  The order never breaks and no two keys collide, so nothing is lost, and the
  cost is length in a text column. The fix is the integer-prefix form of the
  index, where a key carries a magnitude as well as a fraction and an append
  costs no length at all. It is a rewrite of one function and its property
  test, and it is not needed until a person reorders a long list every day.

- **Two label rows can hold one name.** `setLabels` matches a label by name,
  and `label_add` trusts the id it is given. A `label_add` for a name that a
  quick add created inline, or a `task_update` that names a label after a
  `label_delete`, makes a second row with the same name. Only the importer
  sends `label_add` today, and it sends every label before every task, so the
  path is not reachable from a client. The answer is a unique index on the live
  name, which is a schema change. `TestPropertyCommandsCommute` keeps the two
  name spaces apart on purpose and says why.

- **A `project` row and a `label` row carry no `source_ref`.** Only a task
  does. The importer therefore matches a project and a label by name, so a
  project renamed in Todoist after an import arrives as a second project. The
  answer is a `source_ref` column on both tables, which is a schema change.

## The filter language

- **There is no comment table, so `comment:` searches the description.** A
  comment lives in the description today: the importer folds one in and every
  client writes one there. The term points at the right column and means the
  right thing, and it needs one line of change on the day the table arrives.
  §6.3 of [PLAN.md](PLAN.md) counts "query comment text" as closed only when a
  comment is a row.

- **`with subtasks:` takes one term, not a group.** The lexer splits a query on
  `&`, `|` and the parentheses before a term is read, so the value of a
  `key: value` term cannot hold an operator. `with subtasks: p1 & #Home` reads
  as a family of `p1`, and then `& #Home`, which is the useful reading and not
  the only possible one. A group needs a real unary operator in the grammar,
  and that needs a character the lexer does not already use for a relative date
  such as `after: +5 days`.

- **No client offers the three gap terms in its own view list.** The grammar
  answers them everywhere, and a person has to type them.

## The importer

- **A saved filter is read and never written.** There is no `filter` table. The
  importer names every filter and prints every query in the summary, so a
  person can type them back in, and the grammar is the same grammar. A filter
  table is Phase 1 work in the plan and it needs a schema change.

- **A section is folded into the description.** There is no `section` table.
  The name arrives as the first line of the description and the summary counts
  it. Recorded in [POC.md](POC.md) already.

- **A project comment is counted and dropped.** Our model has no comment on a
  project. The summary says how many.

- **The archive never arrives.** A Todoist full sync sends the count of
  completed tasks per project and never the tasks. §4 of the plan lists
  "completed history" as part of the import, and what arrives is a completed
  task that is still in the active payload plus a count of the rest. Reading
  the real archive needs the completed-items endpoint and its own paging, and
  the fixture cannot rehearse it until that code exists.

## The quick add parser

- **A date phrase inside quotes does not stay literal.** §6.4 of
  [PLAN.md](PLAN.md) states the rule and neither parser has it, so
  `Read "tomorrow never dies"` loses the words from the title and takes a due
  date nobody asked for. The fix is a mask over every quoted span before the
  patterns run, in the Go parser and in `parse.js` together, plus a fixture in
  `parser-fixtures/quickadd.json` so the two cannot drift. It is small and it
  touches the web asset, which another milestone is editing.

- **`/section` is not parsed at all.** §6.4 names `#`, `@` and `/`. There is no
  section table, so a parsed `/section` would have nothing to resolve against.

- **A `#` or an `@` is consumed with no account to check it against.** §6.4
  says a token is consumed only when it matches something in the account. The
  parser has no account, so it always consumes, and each caller decides what an
  unknown name means. All three callers now agree that it means the inbox or a
  clear refusal, and `TestZeroLossHandlesTheUnknownProjectTheSameWayEverywhere`
  locks that.
