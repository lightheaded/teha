# Research 02 — Features other apps have and their communities praise

*2026-08-25. Research pass completed for all 17 apps in scope, with a vendor source and a user-voice source for each praised feature. Reddit search results came back through DuckDuckGo Lite; some Reddit pages could not be fetched directly, so the citation is the search result snippet plus the thread URL. Claims without a source found this session are marked UNVERIFIED.*

## Per app

### Godspeed (macOS, iOS)

- Every interaction runs under 50 ms. The app is 100% keyboard driven. "Hardcore mode" turns off the mouse.
- The command palette (`Cmd+K`) lists every command with its hotkey.
- Macros are user-made commands built from actions with variables (`currentDate`, `clipboard`, prompts). A hotkey, the palette, or a URL can trigger a macro.
- Key chords (leader keys) exist. Hotkeys are fully editable and sync across devices.
- Start dates are separate from due dates. Deferred tasks show in gray. Snooze keeps the repeat schedule.
- Boolean search uses operators (`label:bug`, `due:today`, `duration<15m`, `group:list`, `sort:due`, `limit:10`). Smart lists are saved searches. A task made inside a smart list inherits its criteria.
- The app stores data in local SQLite, works fully offline, and offers optional cloud sync, opt-in end-to-end encryption, and JSON and Markdown export.
- Each list gets its own email address for email-to-task capture.
- Natural language dates work in the title field: `3d`, `2 hours and 15 mins`, `Oct 14 at noon`.
- Criticism: the price, a Mac-first release schedule, proprietary sync, and no CalDAV.

Sources: [godspeedapp.com guides](https://godspeedapp.com) (command palette, macros, hotkeys, start dates, snoozing, search operators, smart lists, encryption), [Hacker News thread](https://news.ycombinator.com/item?id=39756325).

### TickTick

- The app includes a habit tracker, a Pomodoro timer, and an Eisenhower matrix. It logs task duration.
- A dedicated "Won't do" state exists next to "Done."
- Voice Capture uses natural language: "Speak naturally to capture tasks. AI automatically extracts dates, priorities, and creates multiple tasks at once."
- The app offers a sticky note widget, smart lists, and kanban sections. It syncs in real time across phone, computer, tablet, and watch.
- Criticism: cluttered settings, sync delays, and data ownership concerns from users.

Vendor source: [ticktick.com](https://www.ticktick.com/). User voice: an Android Police review reported "Tracking habits got easier. The Pomodoro timer helped." — [androidpolice.com, July 2025](https://www.androidpolice.com/replaced-to-do-list-habit-tracker-planner-with-ticktick/).

### OmniFocus 4

- Defer dates mark items "not available yet" until a set date. Due dates mark hard deadlines. The two fields are separate. See [omnigroup.com/omnifocus/features](https://www.omnigroup.com/omnifocus/features).
- The Review feature reminds users on a set interval to go through each project and check it is on track. Same source.
- Custom Perspectives filter and group projects and tags into a saved view, for example everything tagged "Grocery Store" and "Birthday." Perspectives are a Pro feature. Same source.
- Sequential versus parallel projects and hard task dependencies are well-known OmniFocus features, but no working vendor page confirmed the exact mechanism this session. **UNVERIFIED**.
- A user planning a defer/due date setup on the Omni forum wrote about a "Daily Shutdown Review" step and how defer and due dates split into separate planning steps — [discourse.omnigroup.com, August 2025](https://discourse.omnigroup.com/t/ideas-for-new-setups-with-defer-planned-date-and-due-date/71199). PCMag called OmniFocus "an ideal to-do-list app for Apple power users who follow the Getting Things Done method and want to customize every last detail" — [pcmag.com](https://www.pcmag.com/reviews/omnifocus).
- Criticism: price, complexity, and an Apple-only platform.

### Amazing Marvin

- Strategies are individual features a user switches on one at a time, for example time blocking, duration estimates, and a week scheduler. See [help.amazingmarvin.com/strategies](https://help.amazingmarvin.com/en/collections/1139197-strategies).
- The Task Jar lets randomness pick the next task: "Add tasks to the jar and let it randomly select what you work on next" — [amazingmarvin.com](https://amazingmarvin.com/).
- Spotlight focuses a user on one to three hours of work with suggested or custom tasks. Day Planning gives "instant feedback on whether your plan is realistic or over capacity." Same source.
- The Procrastination Wizard walks a stuck user through a task step by step. Same source.
- A grad student on Reddit wrote: "I love all the fancy features and strategies, but that right there is what has kept me from having a complete breakdown" — [reddit.com/r/productivity, undated](https://www.reddit.com/r/productivity/comments/e117ps/amazing_marvin_is_the_best_todo_app_ive_ever_used/). A new user recommended turning on "time blocking, duration estimates, planning ahead, time block sections... and the week scheduler" — [reddit.com/r/amazingmarvin](https://www.reddit.com/r/amazingmarvin/comments/qmx57x/hey_i_am_just_discovering_am_and_decided_to_move/).

### Sunsama

- Sunsama lets a user "timebox tasks directly on your schedule" and merges Google Calendar, Outlook, and Apple Calendar with two-way sync. See [sunsama.com](https://www.sunsama.com/).
- The daily planner asks the user to align goals, set priorities, and set a realistic workload before the day starts. Same source.
- A Daily Shutdown ritual closes the day, tracks wins, and helps the user "finish the day feeling calm and accomplished." Same source.
- Sunsama pulls tasks from Slack, Todoist, Trello, Asana, ClickUp, Jira, and Linear into one list. Same source.
- A three-year user wrote: "I've tried many different apps over the years... Things 3 was my previous favourite but over time I felt that tasks kept piling up and weren't being completed" — [reddit.com/r/productivity](https://www.reddit.com/r/productivity/comments/x6357l/sunsama_price_worth_it/).
- Criticism: price (about $20 to $25 a month) and a workflow built around one task at a time rather than fast capture.

### Akiflow

- The Universal Inbox pulls tasks from more than 3,000 tools into one place. See [akiflow.com](https://akiflow.com/).
- Time blocking lets a user "allocate specific periods in your calendar for focused work." Same source.
- Akiflow merges Google Calendar, Outlook, Gmail, Slack, Microsoft Teams, and Zoom into one calendar view. Same source.
- A Rituals assistant guides a daily review of the day before and a plan for the day ahead. Same source.
- A Reddit user wrote: "Once you need to integrate Linear, JIRA, specific emails... and time box those in your personal or work calendars then Akiflow starts to really shine against any other platform" — [reddit.com/r/ProductivityApps](https://www.reddit.com/r/ProductivityApps/comments/1b3jmu7/definitive_answer_akiflow_is_the_best_todo_list_/).
- Criticism: price (about $34 a month).

### Motion

- Motion schedules tasks into open calendar slots by priority, dependency, deadline, and duration. See [usemotion.com](https://www.usemotion.com/).
- The schedule updates "dozens of times a day, all done automatically" and re-plans the day whenever a priority changes. Same source.
- Motion merges Outlook, Google, and iCloud calendars into one interface to prevent double-booking. Same source.
- A user wrote: "I can give it my tasks and their priority/deadline and it will automatically put it on my calendar in an open slot. If I don't get to a task it automatically reschedules it back into my calendar at a future time" — [reddit.com/r/productivity](https://www.reddit.com/r/productivity/comments/186spn6/motion_app/).
- Criticism: a steep learning curve and price (users report roughly $30 to $35 a month).

### Apple Reminders

- A grocery list "automatically sorts items into categories to make shopping easier" — [Apple Support guide](https://support.apple.com/guide/iphone/make-a-grocery-list-iph80ba26e1f/ios).
- Shared lists, location reminders, and custom Smart Lists with tags are documented Reminders features — [support.apple.com/guide/reminders](https://support.apple.com/guide/reminders/create-list-groups-and-smart-lists-icl1013/mac).
- A parent wrote about the grocery list: "My iPhone shopping list just magically expanded to include gummy bears and chips... They don't need to call or ask me for this. They simply add items to the Apple Reminders shopping list from home" — [iproductivity.substack.com, January 2026](https://iproductivity.substack.com/p/the-simplest-way-to-start-using-apple).
- Criticism: aisle order does not adapt to more than one store, and category sorting sometimes stops working — [MacRumors forum thread](https://forums.macrumors.com/threads/reminders-grocery-list-and-multiple-stores.2446890/).

### Google Tasks

- Google Tasks shows in Gmail, Calendar, Chat, and Docs. A user can "turn an email into a to-do so it doesn't slip through the cracks" — [workspace.google.com/products/tasks](https://workspace.google.com/products/tasks/).
- A task with a date and time appears automatically on Google Calendar, with an option to mark the user as busy. Same source.
- Tasks support subtasks, a star for priority, and recurrence (daily, weekly, monthly, annually). Same source.
- A home screen widget exists on Android and iOS — [support.google.com/tasks/answer/10478781](https://support.google.com/tasks/answer/10478781).
- A user wrote: "The widget at least on the Pixel performs pretty well. I use it for small project management, reminders, and daily to-do lists. Integration with Calendar is..." — [reddit.com/r/google](https://www.reddit.com/r/google/comments/1532xql/i_almost_cant_believe_it_but_google_tasks_is/).
- Criticism: no natural-language quick add and few power-user filters compared with dedicated apps.

### Bring! and AnyList (shopping)

- Bring! lets a user add a recipe's ingredients to a shopping list "with just one click" and shares lists live across a household — [getbring.com/en/features](https://www.getbring.com/en/features).
- A couple on Reddit praised "the handling with icons and sortable areas in the shop as well as the addable info (amount, type and so on)" — [reddit.com/r/selfhosted](https://www.reddit.com/r/selfhosted/comments/u4uy5j/bring_shopping_list_alternative/).
- AnyList "suggests common items as you type, and automatically groups items by category to help save time at the store" — [anylist.com](https://www.anylist.com/).
- A parent on Reddit wrote: "The free version allows you to create list and share with your spouse. So easy to make a list and it updates for both. Easy to check off and see what they have already got or need" — [reddit.com/r/dadditchefs](https://www.reddit.com/r/dadditchefs/comments/14fqzja/anylist_is_a_game_changer/).
- Neither app tracks "who buys" a specific item; both rely on live sync and check-off state to avoid duplicate purchases. **UNVERIFIED** whether either app has a formal item-assignment field.

### Tasks.org (Android, open source)

- Tasks is "free software, licensed under the GNU GPLv3." It supports "Tasks.org, Google Tasks, DAVx⁵, CalDAV, EteSync, or DecSync CC" as sync backends — [tasks.org](https://tasks.org/).
- A user can "use offline, self-host, or setup EteSync for end-to-end encryption," and the app carries no ads and shares no data — same source.
- A Reddit user listed field-order customization, custom filters, default task durations, and location-based tasks as advantages over Microsoft To Do — [reddit.com/r/Android](https://www.reddit.com/r/Android/comments/gqq5a0/tasksorg_opensource_todo_lists_reminders/).
- A quick settings tile opens new-task creation without leaving the current app (`TileServiceCompat.startActivityAndCollapse`), confirmed by source inspection in a prior pass.

### Structured

- Structured lays out a day as "a single visual timeline," with up to 99 appointments a day, weekly and monthly views, and calendar import — [structured.app](https://structured.app/).
- The app syncs a plan across phone, computer, and smart watch. Same source.
- A user wrote: "It shows your schedule in a sort of linear way that makes it so easy to check things off as you go... it also has widgets and whatnot that really help with keeping an eye on your tasks" — [reddit.com/r/productivity](https://www.reddit.com/r/productivity/comments/13xqsy1/structured_app_ios_a_productivity_lifesaver/).
- Criticism from the same community: time estimates run short for real tasks, and a newer weekly view is harder to use for direct scheduling — [reddit.com/r/structuredapp](https://www.reddit.com/r/structuredapp/top/?t=all).

### Superlist

- Notes and tasks live in one document. A task can be created directly from note text.
- Real-time shared lists support comments, mentions, and attachments.
- Integrations cover Gmail, Google Calendar, Slack, GitHub, Linear (two-way status), and email forwarding.
- Criticism: a slow start (one to two minutes reported), bugs, too much animation, and a heavy feel for solo use.

Sources: [superlist.com](https://superlist.com), [help.superlist.com](https://help.superlist.com), [alternativeto.net/software/superlist](https://alternativeto.net/software/superlist).

### Linear (team issue tracker, often used for personal tasks)

- The sync engine applies every change locally first: an object pool in memory, an IndexedDB cache, a transaction queue, delta packets over WebSocket, and one global `lastSyncId`. Conflicts resolve last-write-wins because they are rare.
- The app shows no loading states through partial bootstrap and lazy hydration.
- Single-key shortcuts (`C` create, `S` status, `P` priority, `A` assignee, `L` labels), `G` chords for navigation, and `Cmd+K` for everything else drive the interface.
- A triage inbox lets a user accept, mark duplicate, decline, or snooze an item with one key.
- Cycles roll unfinished work forward automatically.
- Caveat: a cold offline cache shows a blank window on first load.

Sources: [linear.app/now/scaling-the-linear-sync-engine](https://linear.app/now/scaling-the-linear-sync-engine), [reverse-linear-sync-engine on GitHub](https://github.com/wzhudev/reverse-linear-sync-engine), [linear.app/docs/triage](https://linear.app/docs/triage), [linear.app/method](https://linear.app/method).

### Obsidian Tasks plugin

- Emoji fields mark each date type: due, scheduled, start, created, done, cancelled. Priority uses four emoji levels.
- Query blocks (` ```tasks `) support filters, sort, group, limit, boolean logic, a global query, and per-file defaults.
- Recurrence uses the syntax `🔁 every week when done`. It has no count limit and no until date.
- Dependencies use an `🆔` id field and an `⛔` blocker field, with an `is not blocked` query filter.
- A global filter tag separates tasks from ordinary checkboxes.
- Complaints: no reminders, no widgets, no quick capture, a query syntax that takes time to learn, and results that sometimes go stale.

Sources: [publish.obsidian.md/tasks](https://publish.obsidian.md/tasks), [github.com/obsidian-tasks-group/obsidian-tasks](https://github.com/obsidian-tasks-group/obsidian-tasks).

### Vikunja

- Vikunja is "open-source under the AGPLv3" and lets a user "run Vikunja on your own infrastructure" — [vikunja.io](https://vikunja.io/).
- Relations connect tasks across projects; a relation "can be multiple things, for example a subtask or blocking task" — [vikunja.io/features](https://vikunja.io/features).
- Tasks can be assigned to specific users, and a Family plan adds shared projects for up to five household members. Same source.
- Four view types exist: list, kanban, Gantt, and table.
- A user researching a Todoist replacement wrote: "except for the particular use-cases I mentioned in my OP, Vikunja looked to be the most similar alternative to ToDoist without the subscription" — [reddit.com/r/Vikunja](https://www.reddit.com/r/Vikunja/comments/17uf1cr/keen_to_try_vikunja_self_hosted_as_replacement/). XDA Developers called it an alternative that "feels lighter and more powerful" — [xda-developers.com, August 2025](https://www.xda-developers.com/open-source-alternative-to-todoist/).
- Criticism: mobile apps trail the web app in feature parity — one user called the Android app "alpha" and reported no iOS app at the time of writing — [reddit.com/r/selfhosted, March 2025](https://www.redditmedia.com/r/selfhosted/comments/1jfrdzw/taskstodos_thinking_about_going_back_to_todoist/).

## Cross-app list: what people want that Todoist lacks

Ranked by how often the feature appears across the apps above. Phase 1 = solo daily drive. Phase 2 = two people. Phase 3 = beyond Todoist parity. Phase 4 = open source and self-hosted.

| # | Feature | Seen in | Worth copying | Phase |
|---|---|---|---|---|
| 1 | Instant local-first feel: no spinner, keyboard-first, `Cmd+K` palette | Linear, Godspeed, Superlist | Yes — sets the daily-use bar | 1 |
| 2 | Start/defer date separate from due date, with snooze that keeps recurrence | OmniFocus, Godspeed, Marvin | Yes — core to a personal system | 1 |
| 3 | Local-first data with full offline and a sync engine that never loses an edit | Linear, Godspeed, Tasks.org | Yes — matches the two-person, self-hosted goal | 1 |
| 4 | Task dependencies / blocking relation | Vikunja, Linear, Obsidian Tasks | Yes — needed for sequential chores and projects | 3 |
| 5 | Time blocking merged with a real calendar | Sunsama, Akiflow, Motion, Structured | Yes for calendar merge; auto-scheduling is a stretch goal | 3 |
| 6 | Notes attached to a task or project | Superlist, Obsidian Tasks | Yes — cheap to add, high value | 1 |
| 7 | Review ritual (per-project or daily shutdown) | OmniFocus, Marvin, Sunsama, Akiflow | Yes — a saved view plus a reminder, not a new subsystem | 2 |
| 8 | Shopping list mode: category grouping, live shared edits, fast check-off | Apple Reminders, Bring!, AnyList | Yes — direct fit for a two-person household | 2 |
| 9 | Habits and routines with streaks | TickTick, Structured | Maybe — useful but separate from task management | 3 |
| 10 | Self-hosting and end-to-end encryption or local storage | Vikunja, Tasks.org, Godspeed, Obsidian Tasks | Yes — matches the "open and hosted" goal directly | 4 |
| 11 | Per-item "pick a task for me" / random or suggested task | Marvin (Task Jar, Spotlight) | Maybe — a small feature, high delight for an AI agent to drive | 3 |
| 12 | Per-list email address or email-to-task | Godspeed | Maybe — low cost, niche use | 3 |
| 13 | Widget and quick settings tile for fast capture | Tasks.org, TickTick, Google Tasks | Yes — needed for daily mobile use | 2 |
| 14 | Household task assignment and per-person views | Vikunja (Family plan), Apple Reminders (shared lists) | Yes — required for the two-person target | 2 |
| 15 | Time estimates and a day-fits-in-my-day check | Marvin, Sunsama | Maybe — useful once time blocking exists | 3 |
| 16 | Aisle/category auto-sorting for shopping items | Apple Reminders, AnyList | Yes — small win, high daily payoff | 2 |
| 17 | Natural-language quick add | TickTick, Things 3, Godspeed | Yes — an AI agent can implement this well | 1 |
| 18 | "Who buys this" assignment on a shopping item | **UNVERIFIED** in Bring! or AnyList — neither vendor page confirmed a formal assignment field | Maybe — worth a custom field if the vendors truly lack it | 3 |

## Notes on draft accuracy

- The prior draft's Godspeed, Linear, Obsidian Tasks, and Superlist sections held up under a fresh check this session and are kept largely unchanged.
- The prior draft listed OmniFocus dependencies and sequential/parallel projects as confirmed fact from "product knowledge." This session could not reach a live Omni Group page describing the exact mechanism; the claim is now marked UNVERIFIED rather than stated as fact.
- The prior draft grouped Sunsama, Akiflow, and Motion into one unsourced paragraph. Each now has its own section with a vendor page and a named Reddit or review source.
- Bring! and AnyList: the draft implied a "who buys" field exists. Neither vendor page found this session states a formal buyer-assignment field; this is now marked UNVERIFIED rather than assumed.
