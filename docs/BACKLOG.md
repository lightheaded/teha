<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->

# Backlog

Everything that a change left knowingly unfinished, with the reason. A line
leaves this file when the work lands. See [PLAN.md](PLAN.md) section 9.

## Sections and the board layout, 2026-08-27

- **`docs/screenshots/board.png` is not generated.** `scripts/screenshots.mjs`
  captures the board now, and the image is not in `docs/screenshots/`, because
  Docker does not run in the environment that wrote the layout. The screenshots
  workflow fails until somebody runs `scripts/screenshots.sh` once and commits
  the new file. Nothing else in the repository points at the missing image, so
  no README shows a broken picture. The six older images are untouched, and the
  seed change was written to keep them identical.
- **The list layout has no drag.** Only the board does. A list row is sorted by
  the due date, then the priority, then the title, so a drag in the list would
  write an order key that no view reads. Give the list an order-key sort first,
  then reuse the board drag.
- **Quick add does not read `/Section`.** PLAN.md section 6.4 lists a section in
  the parser output. `quickadd` is Apache-2.0 and shared with the phone, so the
  term needs a fixture in `parser-fixtures/quickadd.json` and both parsers, which
  is a change of its own.
- **The phone has no board and no section field.** The Android client keeps the
  same rows in Room under different names, so `section_id` needs a column there
  and a mapping in the sync code.
- **The command line client cannot name a section.** `teha add` and `teha ls`
  pass a filter through, so `/Section` already works in `teha ls`. Writing a
  section from the command line does not.
- **A deleted project keeps its sections live.** `project_delete` is a soft
  delete and it already leaves its tasks alone, so the sections follow the same
  rule. A restore therefore brings the whole project back. Revisit with a real
  hard delete.
