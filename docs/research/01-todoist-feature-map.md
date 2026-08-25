# Research 01 — Todoist feature map

*Updated 2026-08-25. Sources: the Todoist help center, developer.todoist.com/api/v1, and community threads on Reddit. Every non-obvious claim carries a source link. Items with no confirmed source carry the mark **UNVERIFIED** and a note on what the agent tried.*

This is the parity checklist. Every row is a candidate feature for the clone. The **Tier** column names the Todoist plan that has the feature. As of August 2026 the personal plans are **Beginner** (the renamed free tier), **Pro**, and **Business** ([pricing and billing FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6)). The column keeps the letters **F** (Beginner), **P** (Pro), **B** (Business) from the earlier draft, but F now means Beginner, not Free. The **Phase** column is the proposal from `docs/PLAN.md`.

## 0. Plan limits (August 2026)

Source: [usage limits in Todoist](https://www.todoist.com/help/articles/usage-limits-in-todoist-e5rcSY) and the [plans, pricing, and billing FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6). Quote every number before you publish it elsewhere; Todoist changes these often.

| Limit | Beginner | Pro | Business |
|---|---|---|---|
| Price | Free | $7/month, or $60/year | $10/user/month, or $8/user/month billed yearly |
| Active tasks per project | 300 (all plans) | 300 | 300 |
| Sections per project | 20 (all plans) | 20 | 20 |
| Active personal projects | 5 | 300 | 300 per member |
| Active team projects | 5 | 5 | Up to 500 |
| Labels per account | 500 (all plans) | 500 | 500 |
| Labels per task | 100 (all plans) | 100 | 100 |
| Collaborators per project | 5 personal / 250 team (all plans) | same | same |
| Filters | 3 | 150 | 150 (team filters) |
| Reminders | Automatic only, 700 | Automatic and custom, 700 combined | Automatic and custom, 700 combined |
| File upload size | 5 MB (email attachment) | 25 MB (email attachment) | 100 MB (comment attachment) |
| Activity history | 1 week | Full | Full |
| Calendar layout, task durations, time-blocking | Not included | Included | Included |
| Ramble (voice-to-tasks, AI) | 10 sessions/month | Unlimited | Unlimited |
| AI features (Todoist Assist, Email Assist) | Not included | Included | Included |
| Backups | Not documented on this page | Not documented on this page | Not documented on this page — **UNVERIFIED**, see note below |

Note on backups: the source pages for this table do not list a per-plan backup limit. The API docs mention a `Backups` endpoint but the agent did not load it this session. Mark this row **UNVERIFIED** until someone reads [developer.todoist.com/api/v1](https://developer.todoist.com/api/v1/) under the "Backups" heading.

The API's own workspace object confirms two team plan codes, `teams_workspaces_starter` (Beginner Team) and `teams_workspaces_business` (Business), each carrying a `limits.current` and `limits.next` block with fields such as `max_projects`, `max_collaborators`, `upload_limit_mb`, `automatic_backups`, `calendar_layout`, and `reminders` ([developer.todoist.com/api/v1](https://developer.todoist.com/api/v1/)). This is useful as a schema reference for the clone's own plan-limit model, even where the clone has no paid tiers.

## 1. Capture

| Feature | Detail | Tier | Phase |
|---|---|---|---|
| Quick add | One text field. Natural language for every field. | F | 1 |
| Date parsing | `tomorrow`, `next mon`, `14 sep`, `in 3 days`, `sat 9am`, `end of month`, `2026-09-01` | F | 1 |
| Time parsing | `at 15:00`, `3pm`, `noon`, `tonight` | F | 1 |
| Recurrence | `every day`, `every workday`, `every weekend`, `every 3 weeks`, `every 2nd tue`, `every last day`, `every jan 15`, `every! 2 weeks` (from completion), `every day starting 1 sep`, `every mon ending 1 dec`, `every day for 3 weeks` | F | 1 |
| Priority | `p1`..`p4` | F | 1 |
| Project | `#Groceries`, `#"Trip to Rome"` | F | 1 |
| Section | `/Produce` | F | 1 |
| Label | `@errand`, several per task. Quick Add still accepts `@label`; this is a separate token set from the filter query language, see section 4. **UNVERIFIED**: the agent could not confirm from the Quick Add help page this session whether `@` in Quick Add faces the same 2026 retirement as `@` in filters. | F | 1 |
| Assignee | `+Name` in a shared project | F | 2 |
| Reminder | `!30min`, `! tomorrow 9am`, relative to due time. Automatic reminders now ship on Beginner too; custom, time-based, and location reminders stay Pro/Business ([plans FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6)). | F (automatic) / P (custom) | 2 |
| Duration | `for 30min`, `for 2h` | P (Beginner has no durations, [plans FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6)) | 2 |
| Deadline | `{14 sep}` in braces, separate from due date | F | 2 |
| Description | Second field under the title, Markdown | F | 1 |
| Autocomplete | Menu appears while you type `#`, `@`, `/`, `+` | F | 1 |
| Date highlight | The parsed date is highlighted in the field, tap to cancel the parse | F | 1 |
| Default project | Quick add remembers the last project, or uses Inbox | F | 1 |
| Email to project | Each project has a mail address. Subject becomes the title, body becomes the description. | F | 3 |
| Browser extension | Save the page as a task | F | 3 |
| Share sheet | Android and iOS share target | F | 1 |
| Widgets | Task list widget, add button widget, Android quick settings tile | F | 1 |
| Voice | Siri, Google Assistant, Wear OS, Apple Watch. Ramble (AI voice-to-tasks) is a newer, separate feature: 10 sessions a month on Beginner, unlimited on Pro/Business ([plans FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6)). | F | 4 |
| Multiple tasks | Paste several lines, one task per line | F | 1 |

## 2. Task model

| Feature | Detail | Tier | Phase |
|---|---|---|---|
| Sub-tasks | Nested several levels. Parent shows a counter. Completion of a parent asks about open children. | F | 1 |
| Priority | Four levels, colored flags | F | 1 |
| Due date and time | With timezone, floating or fixed | F | 1 |
| Recurring rule | Stored as a natural language string, next date computed on completion | F | 1 |
| Deadline | Hard date, separate from the planned date | F | 2 |
| Duration | Minutes, used by the calendar layout. Pro and Business only ([plans FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6)). | P | 2 |
| Labels | Personal labels, shared labels in workspaces, colors, favorites. Account cap: 500 labels, 100 per task ([usage limits](https://www.todoist.com/help/articles/usage-limits-in-todoist-e5rcSY)). | F | 1 |
| Description | Markdown, links, checklists | F | 1 |
| Comments | Text, attachments (files, images, audio), reactions, per task and per project. Comment text caps at 15,000 characters. Attachment size: 5 MB on Beginner, 25 MB on Pro (email), 100 MB on Business (comment attachment) ([usage limits](https://www.todoist.com/help/articles/usage-limits-in-todoist-e5rcSY)). | F (limits) | 2 |
| Reminders | At time, before due, at location. Automatic reminders: all plans, 700 cap. Custom, location, and recurring reminders: Pro/Business ([plans FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6)). | F (automatic) / P (custom, location) | 2 |
| Activity log | Who changed what, per task and per project. History window: 1 week on Beginner, full on Pro/Business ([usage limits](https://www.todoist.com/help/articles/usage-limits-in-todoist-e5rcSY)). | F (1 week) / P (full) | 2 |
| Completed history | Completed view per project, per day, with search | F | 1 |
| Task links | `todoist://task?id=` and web URL, copy link. Confirmed URL scheme entries: `todoist://`, `todoist://today`, `todoist://upcoming`, `todoist://profile`, `todoist://inbox`, `todoist://teaminbox` ([developer.todoist.com/api/v1, Url schemes](https://developer.todoist.com/api/v1/)). | F | 1 |
| Move, duplicate | Move to project and section, duplicate with children | F | 1 |
| Multi-select | Bulk reschedule, move, label, complete, delete | F | 1 |
| Undo | Toast with undo after complete, delete, move | F | 1 |
| Uncompletable tasks | Title starts with `* ` — a heading or a note inside a list. The filter keyword is `uncompletable` ([introduction to filters](https://www.todoist.com/help/articles/introduction-to-filters-V98wIH)). | F | 1 |
| Markdown in title | Bold, italic, links `[text](url)` | F | 1 |

## 3. Organization

| Feature | Detail | Tier | Phase |
|---|---|---|---|
| Inbox | Default project | F | 1 |
| Projects | Color, favorite, archive, sub-projects (nested). 300 active projects per member on Pro and Business, 5 on Beginner ([plans FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6)). | F (5 active) / P, B (300) | 1 |
| Sections | Groups inside a project, collapse, reorder. Cap: 20 sections per project on every plan ([usage limits](https://www.todoist.com/help/articles/usage-limits-in-todoist-e5rcSY)). | F | 1 |
| Layouts | List, board (sections as columns), calendar. Calendar layout needs Pro or Business ([plans FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6)). | F / P for calendar | 1 (list), 2 (board, calendar) |
| Sort and group | By date, priority, name, assignee, added date. Group by project, label, date. | F | 1 |
| Favorites | Projects, labels, filters pinned in the sidebar | F | 1 |
| Templates | Gallery of templates, export and import a project as a template. Business workspaces get a separate `max_workspace_templates` cap in the API's workspace-limits object ([developer.todoist.com/api/v1](https://developer.todoist.com/api/v1/)). | F | 3 |
| Workspaces | Shared spaces with members, roles, shared labels and filters. Team plans are named Beginner Team and Business; the API workspace object exposes `max_workspace_users`, `max_guests_per_workspace`, and similar caps ([developer.todoist.com/api/v1](https://developer.todoist.com/api/v1/)). | B / Teams | 2 (household space only) |
| Sharing | Invite by email per project, up to a member limit per tier: 5 collaborators per project on personal plans, 250 on team plans ([usage limits](https://www.todoist.com/help/articles/usage-limits-in-todoist-e5rcSY)). | F | 2 |
| Assignments | One assignee per task, "assigned to me" filter | F | 2 |
| Project comments | Per-project discussion | F | 3 |

## 4. Views and filters

| Feature | Detail | Tier | Phase |
|---|---|---|---|
| Today | Overdue section plus today, reschedule overdue in one action. Today, Upcoming, and Priority are built-in views; a user cannot edit them with filter queries ([introduction to filters](https://www.todoist.com/help/articles/introduction-to-filters-V98wIH)). | F | 1 |
| Upcoming | Scrollable days, week strip, drag between days | F | 1 |
| Filters | Saved queries, colors, favorites. Cap: 3 filters on Beginner, 150 on Pro and Business ([usage limits](https://www.todoist.com/help/articles/usage-limits-in-todoist-e5rcSY)). | F (3) / P, B (150) | 1 |
| Filter Assist | Describe the wanted tasks in English or Spanish; Todoist writes the filter query for you. | F | 4 |
| Filter language — corrected | The label prefix changed. Filters now use `%label` (for example `%email`, `%urgent*`). `@label` still works in filters for now, including filters saved earlier, but Todoist plans to retire `@` in filters by the end of 2026 ([introduction to filters](https://www.todoist.com/help/articles/introduction-to-filters-V98wIH)). Quick Add still uses `@label` to attach a label to a new task; that is a separate token set from the filter query language. | F | 1 |
| Filter language — dates and deadlines | `today`, `tomorrow`, `overdue`/`od`, `date: Jan 3`, `date before:`, `date after:`, `no date`, `no time`, `deadline: today`, `deadline before:`, `deadline after:`, `no deadline`, `due before:`, `due after:`, `recurring`, `!recurring`. Relative forms: `3 days`, `-3 days`, `+4 hours`. Day names (`Monday`) work directly ([introduction to filters](https://www.todoist.com/help/articles/introduction-to-filters-V98wIH)). | F | 1 |
| Filter language — projects, sections, workspaces | `#Project`, `##Project` (project and sub-projects), `/SectionName` (a section by name, anywhere), `#Project & /Section`, `!/*` (tasks with no section), `workspace: Name`, `##FolderName` for a workspace folder ([introduction to filters](https://www.todoist.com/help/articles/introduction-to-filters-V98wIH)). | F | 1 (core), 2 (workspace) |
| Filter language — priority and search | `p1`..`p3`, `p4` or `No priority` (equivalent; `No priority` only works in filters, not Quick Add), `search: text`, `search: http` | F | 1 |
| Filter language — people | `assigned`, `assigned to: me`, `assigned to: others`, `assigned by: Name`, `shared`, collaborators are matched by their Todoist display name or email, not necessarily their real name ([introduction to filters](https://www.todoist.com/help/articles/introduction-to-filters-V98wIH)). | F | 2 |
| Filter language — created date, subtasks | `created: today`, `created before: -365 days`, `created after:`, `subtask`, `!subtask` | F | 1 |
| Filter language — operators | `&` (AND), `\|` (OR), `!` (NOT), `()` grouping, `,` to show several separate lists in one saved filter, `\` to escape a special character inside a project, section, or label name, `*` wildcard (for example `%home*`, `#*Work`, `/*Work*`) ([introduction to filters](https://www.todoist.com/help/articles/introduction-to-filters-V98wIH)). | F | 1 |
| Filter language — documented gaps | Todoist filters cannot query comment text or existence, cannot show completed tasks, and cannot show a parent task together with its sub-tasks in the same result — only one or the other. `assigned to: nobody` is explicitly unsupported; use `!assigned` instead ([introduction to filters](https://www.todoist.com/help/articles/introduction-to-filters-V98wIH)). | F | — |
| Labels view | Every label with its tasks | F | 1 |
| Search | Full text over title, description, comments, projects | F | 1 |
| Calendar feed | Read-only iCal URL per project or for everything | F | 3 |
| Google Calendar | Two-way sync, since 2024 | F | 4 |
| Completed tasks in Today | Show completed tasks toggle | F | 1 |
| Command menu | `Cmd/Ctrl+K` to jump and run commands (2023) | F | 1 |
| Keyboard shortcuts | `q` quick add, `a` add, `/` search, `g t` today, `1..4` priority, `t` today, `w` week, etc. | F | 1 |

## 5. Mobile

| Feature | Detail | Tier | Phase |
|---|---|---|---|
| Swipe gestures | Right to complete, left to schedule, both configurable | F | 1 |
| Long press | Multi-select, drag to reorder and indent | F | 1 |
| Quick add button | Floating button, opens the quick add sheet | F | 1 |
| Android quick settings tile | Opens quick add from the notification shade | F | 1 |
| Widgets | Task list with filter, add button | F | 2 |
| Notifications | Reminders, daily digest, comments, assignments. Automatic reminders now ship on Beginner; custom reminders need Pro/Business ([plans FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6)). | F (digest, automatic reminders) / P (custom reminders) | 2 |
| Offline | Full read and write offline, sync later | F | 1 |
| Themes | Light, dark, system, accent colors | F / P (more) | 1 |

## 6. Productivity

| Feature | Detail | Tier | Phase |
|---|---|---|---|
| Karma | Points, levels, streaks, daily and weekly goals, vacation mode | F | 4 (optional, off by default) |
| Productivity view | Completed per day, per project, this week | F | 3 |
| Daily digest | Morning email or push with today's list | F | 2 |
| Streaks | Days in a row that met the goal | F | 4 |
| Ramble | AI voice-to-tasks. 10 sessions a month on Beginner, unlimited on Pro/Business ([plans FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6)). | F (10/mo) / P (unlimited) | 4 |
| Todoist Assist, Email Assist | AI task drafting and email-to-task capture. Pro and Business only ([plans FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6)). | P | 4 |

## 7. Integrations and API

| Feature | Detail | Tier | Phase |
|---|---|---|---|
| API v1 | Unified REST plus `/sync` endpoint at `https://api.todoist.com/api/v1/sync`. Replaces REST v2 and Sync v9. `sync_token` model: pass `*` for a full sync, then use the returned token for the next incremental sync. Commands carry client-generated `uuid` and `temp_id`. Authentication: Bearer token in the `Authorization` header; OAuth 2.0 for third-party apps, with refresh tokens the default for new apps ([developer.todoist.com/api/v1](https://developer.todoist.com/api/v1/)). | F | 1 (same model) |
| API request limits — corrected | Documented on the "Request limits" page of developer.todoist.com/api/v1: a **1 MiB** HTTP body limit on POST requests; total header size cannot exceed **65 KiB**; a processing timeout of **15 seconds** for a standard request and **5 minutes** for an upload; for the sync endpoint, **up to 1,000 partial-sync requests per 15 minutes per user** and **up to 100 full-sync requests per 15 minutes per user**; up to **100 commands batched into one sync request**, which still counts as a single request ([developer.todoist.com/api/v1, Request limits](https://developer.todoist.com/api/v1/)). Several third-party sources (a `todoist-mcp` GitHub project, the `td` CLI docs, and an API directory site) instead cite a flat **450 requests per 15 minutes**; the agent could not find that figure on the official page this session, so treat 450/15min as **UNVERIFIED** and prefer the sync-specific 1,000/100 figures above for the clone's own budget planning. | F | 1 (same model) |
| Webhooks | Events on item and project changes | F | 3 |
| MCP server | `https://ai.todoist.net/mcp`. Uses OAuth. Read access to tasks and projects, create and update tasks/projects (rename, reschedule, move). Todoist's own docs confirm ChatGPT, Claude, Cursor, and VS Code as supported clients ([use ChatGPT with Todoist (MCP)](https://www.todoist.com/help/articles/use-chatgpt-with-todoist-mcp-WEeLx9d8h), [developer.todoist.com/api/v1](https://developer.todoist.com/api/v1/)). A separate open-source `Doist/todoist-mcp` server exists on GitHub for self-hosting the same tool set. | F | 1 (own server) |
| CLI | `@doist/todoist-cli` (`td`) | F | 3 |
| Url schemes | `todoist://`, `todoist://today`, `todoist://upcoming`, `todoist://profile`, `todoist://inbox`, `todoist://teaminbox` (redirects to inbox on non-Business accounts) ([developer.todoist.com/api/v1, Url schemes](https://developer.todoist.com/api/v1/)). | F | 3 |
| Zapier, IFTTT, Make | Automation | F | 4 |
| Slack, Gmail, Outlook | Create tasks from messages | F | 4 |
| Backups | Daily export. **UNVERIFIED**: the current per-plan backup limit and cadence were not confirmed this session; the usage-limits page does not list a backups row, though the API docs have a `Backups` endpoint. Read [developer.todoist.com/api/v1](https://developer.todoist.com/api/v1/) under "Backups" to confirm. | P | 1 (export always) |
| Import and export | CSV per project | F | 1 |

## 8. Community complaints about Todoist (2024 to 2026)

Collected from Reddit and the `td` CLI experience. Each row is an opportunity.

| Complaint | Notes |
|---|---|
| No start or planned date | Only a due date. A Reddit thread confirms Todoist has no separate "planned date" field, and users work around it with a label plus a filter ([r/todoist: Deadlines/Due Dates](https://www.reddit.com/r/todoist/comments/154y2ip/deadlinesdue_dates/)). Godspeed, OmniFocus, and Things have a start date. |
| No multi-day or ranged events | A task can carry one due date, or a due date plus a separate deadline, but not a date range for an event that spans several days ([r/todoist: Can I create an event over several days?](https://www.reddit.com/r/todoist/comments/1c7rtd0/can_i_create_an_event_over_several_days/)). |
| No dependencies | Cannot say "B after A". Obsidian Tasks has `⛔`. **UNVERIFIED** this session: draft claim kept, no fresh source found. |
| Sub-tasks in Today lose context | The parent is not shown in list views. **UNVERIFIED** this session: draft claim kept, no fresh source found. |
| Recurring tasks reschedule from the due date | `every!` fixes it, few people know the syntax. **UNVERIFIED** this session: draft claim kept, no fresh source found. |
| Reminders behind Pro, partly fixed | As of August 2026, automatic reminders ship on Beginner too. Custom, location, and recurring reminders, plus the calendar layout, still need Pro or Business ([plans FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6)). This softens, but does not remove, the old complaint. |
| Filters cannot see comments or completed tasks, and cannot combine parent and sub-task in one view | Confirmed directly by Todoist's own "Unsupported filters" table, not just a community complaint ([introduction to filters](https://www.todoist.com/help/articles/introduction-to-filters-V98wIH)). |
| Sync errors and data loss | A September 2025 incident where edits on mobile did not sync. Help center warns that a dismissed sync error can lose changes. **UNVERIFIED** this session: draft claim kept, no fresh source found; re-check the Todoist status page and help center. |
| Web app is slow | HN threads from 2020 to 2024 call the web app slow and mouse-first. **UNVERIFIED** this session: draft claim kept, no fresh source found. |
| No offline web | The web app has no persistent offline storage. **UNVERIFIED** this session: draft claim kept, no fresh source found. |
| API access is fragile | REST v2 deprecation, plus the sync endpoint's per-user caps (1,000 partial / 100 full syncs per 15 minutes, confirmed above) can bite a large account or a busy MCP client. |
| No end-to-end encryption | Godspeed offers it. **UNVERIFIED** this session: draft claim kept, no fresh source found. |
| Karma nags | Users want it off. **UNVERIFIED** this session: draft claim kept, no fresh source found. |
| Limited filter sorting | Sorting inside filters arrived late and is still restricted. **UNVERIFIED** this session: draft claim kept, no fresh source found. |
| Price rises, and the free tier lost its name | The free plan is now called Beginner, not Free; Pro costs $7/month or $60/year, Business $10/user/month ([plans FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6)). **UNVERIFIED**: the agent found no fresh Reddit or HN thread on the rename or a specific price rise this session; treat the community-reaction half of this row as unconfirmed. |
| No time blocking on the free tier | Time-blocking sits behind Pro and Business along with the calendar layout and task durations ([plans FAQ](https://www.todoist.com/help/articles/todoist-plans-pricing-and-billing-faq-Vq2z0HWL6)). |
| Board layout is weak | No swimlanes, no WIP limits. **UNVERIFIED** this session: draft claim kept, no fresh source found. |
| No notes | Descriptions are short. Superlist and Obsidian mix notes and tasks. **UNVERIFIED** this session: draft claim kept, no fresh source found. |
| Label prefix churn | Todoist is mid-migration from `@label` to `%label` in filter queries, with `@` retiring in filters by the end of 2026 ([introduction to filters](https://www.todoist.com/help/articles/introduction-to-filters-V98wIH)). A user with old saved filters, or old muscle memory, hits a breaking change here; the clone can avoid ever needing this kind of prefix migration. |
