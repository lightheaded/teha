// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.data

import android.content.Context
import androidx.sqlite.db.SimpleSQLiteQuery
import io.github.lightheaded.teha.data.db.AccountEntity
import io.github.lightheaded.teha.data.db.CommentEntity
import io.github.lightheaded.teha.data.db.LabelEntity
import io.github.lightheaded.teha.data.db.MetaEntity
import io.github.lightheaded.teha.data.db.OutboxEntity
import io.github.lightheaded.teha.data.db.ProjectEntity
import io.github.lightheaded.teha.data.db.SectionEntity
import io.github.lightheaded.teha.data.db.TaskEntity
import io.github.lightheaded.teha.data.db.TehaDatabase
import io.github.lightheaded.teha.data.net.ApiClient
import io.github.lightheaded.teha.data.net.ApiError
import io.github.lightheaded.teha.data.net.CommandDto
import io.github.lightheaded.teha.data.net.CommentDto
import io.github.lightheaded.teha.data.net.LabelDto
import io.github.lightheaded.teha.data.net.ProjectDto
import io.github.lightheaded.teha.data.net.SectionDto
import io.github.lightheaded.teha.data.net.SyncRequest
import io.github.lightheaded.teha.data.net.SyncResponse
import io.github.lightheaded.teha.data.net.TaskDto
import io.github.lightheaded.teha.parser.Binding
import io.github.lightheaded.teha.parser.ParsedLine
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import java.io.IOException
import java.time.Instant
import java.util.TimeZone

/** The result of one add. */
data class AddResult(
    val ok: Boolean,
    val title: String = "",
    val projectName: String = "",
    val notice: String = "",
)

/**
 * DueChange is one task's new day.
 *
 * dueTime rides with dueDate on purpose. The server accepts a row that holds a
 * time and no day, and no view can print such a row, so a change that takes
 * the day away takes the time with it, and an undo puts both back.
 */
data class DueChange(val id: String, val dueDate: String?, val dueTime: String?)

/**
 * Edit is one field of one task, changed by hand on the detail screen.
 *
 * One type per field, rather than one patch object with every field nullable.
 * A nullable patch cannot say the difference between "leave the deadline
 * alone" and "take the deadline away", and those are the two things a detail
 * screen does most.
 *
 * A null inside a case therefore always means "take it away".
 */
sealed interface Edit {
    data class Title(val value: String) : Edit
    data class Notes(val value: String) : Edit
    data class Priority(val value: Int) : Edit
    data class Project(val id: String) : Edit
    data class Labels(val values: List<String>) : Edit
    data class Repeat(val rrule: String?) : Edit
    data class Due(val date: String?, val time: String?) : Edit
    data class Starts(val date: String?) : Edit
    data class Deadline(val date: String?) : Edit
    /** Assignee names who does the task. A null takes the name away. */
    data class Assignee(val accountId: String?) : Edit
    /** Section files the task under a heading. A null takes it out of one. */
    data class Section(val sectionId: String?) : Edit
}

/**
 * Nudge is something the other person did that this phone has to say out loud.
 *
 * The repository reports the fact and never the sentence: Notifier writes the
 * words, exactly as internal/push does on the server. See DECISIONS.md D-020.
 *
 * The phone has no push transport of its own, so a nudge is found by comparing
 * what a pull brought with what this database already held. The latency is
 * therefore the sync interval and not a second. docs/BACKLOG.md says so.
 */
data class Nudge(
    /** ASSIGNED or COMMENTED. */
    val kind: String,
    val taskId: String,
    val title: String,
    /** The name of the person who did it, or an empty string before the household is known. */
    val who: String,
    /** The text of a comment. Empty for an assignment. */
    val body: String,
) {
    companion object {
        const val ASSIGNED = "assigned"
        const val COMMENTED = "commented"
    }
}

/** The result of one sync. */
sealed interface SyncResult {
    data class Ok(
        val version: Long,
        val rejected: List<String>,
        val nudges: List<Nudge> = emptyList(),
    ) : SyncResult

    data class Failed(val message: String, val unauthorized: Boolean) : SyncResult
}

/**
 * TehaRepository owns the local cache and the outbox.
 *
 * Every write lands in Room first and in the outbox second, so a capture works
 * with no network. Sync then pushes the outbox and pulls the delta in the one
 * round trip that POST /v1/sync offers.
 */
class TehaRepository(context: Context) {

    private val db = TehaDatabase.get(context)
    val settings = Settings(context)
    private val api = ApiClient(settings)
    private val json = Json { encodeDefaults = false; explicitNulls = false }

    // One sync at a time. Two concurrent runs would send the same outbox rows
    // twice, and the second answer would move the version backwards.
    private val syncLock = Mutex()

    val projects: Flow<List<ProjectEntity>> = db.projects().all()
    val sections: Flow<List<SectionEntity>> = db.sections().all()
    val people: Flow<List<AccountEntity>> = db.accounts().all()
    val labels: Flow<List<LabelEntity>> = db.labels().all()
    val outboxCount: Flow<Int> = db.outbox().countFlow()

    /**
     * tasks reads one view.
     *
     * where comes from the shared filter compiler, and args are its arguments
     * in order. The compiler gives a WHERE clause and nothing else, so the
     * sort belongs here.
     *
     * One sort serves every view. An undated task sorts last, then the oldest
     * day first, so an overdue task sits on top of the Today list with no
     * second query and no grouping pass. Priority and the order key break the
     * remaining ties.
     *
     * The arguments bind as text. SQLite gives a bound value the affinity of
     * the column it meets, so a priority still compares as a number.
     */
    fun tasks(where: String, args: List<String>): Flow<List<TaskEntity>> =
        db.tasks().filtered(
            SimpleSQLiteQuery(
                "SELECT * FROM tasks WHERE deletedAt IS NULL AND ($where) " +
                    "ORDER BY (dueDate IS NULL), dueDate ASC, priority ASC, orderKey ASC",
                args.toTypedArray(),
            )
        )

    fun task(id: String): Flow<TaskEntity?> = db.tasks().byIdFlow(id)
    fun subtasks(parentId: String): Flow<List<TaskEntity>> = db.tasks().children(parentId)
    fun comments(taskId: String): Flow<List<CommentEntity>> = db.comments().forTask(taskId)

    /** projectTasks reads one list whole: what is open and what is done. */
    fun projectTasks(projectId: String): Flow<List<TaskEntity>> = db.tasks().inProject(projectId)

    suspend fun projectsNow(): List<ProjectEntity> = db.projects().allNow()

    /**
     * inboxId reads the project a capture with no project belongs in.
     *
     * Each account has its own inbox, so the fixed id is only right for the
     * owner. The sync answer names the right one and Settings keeps it. The
     * is_inbox flag on a pulled project is the second source, and the fixed id
     * is the last one, for a phone that has never synced.
     */
    suspend fun inboxId(): String {
        val known = settings.inboxId
        if (known.isNotEmpty()) return known
        return db.projects().allNow().firstOrNull { it.isInbox }?.id ?: FALLBACK_INBOX
    }

    /** me is the account this phone is, or an empty string before the first sync. */
    fun me(): String = settings.accountId

    suspend fun version(): Long = db.meta().get(KEY_VERSION)?.toLongOrNull() ?: 0L

    suspend fun testConnection(): String {
        val health = api.health()
        val response = api.sync(SyncRequest(since = 0))
        return "The server answered. Version $health, " +
            "${response.projects.size} projects, ${response.tasks.size} tasks."
    }

    // --- writes -------------------------------------------------------------

    /**
     * add turns one quick add line into a local task and a queued command.
     *
     * The project name resolves here and the command carries project_id. The
     * server refuses an unknown `project` name and drops the command with it,
     * so a typo must never reach the wire as a name.
     */
    suspend fun add(parsed: ParsedLine): AddResult {
        val title = parsed.title.trim()
        if (title.isEmpty()) return AddResult(ok = false, notice = "Write a title.")

        val inbox = inboxId()
        var projectId = inbox
        var projectName = ""
        var notice = ""
        when (val match = matchProject(projectsNow(), parsed.project)) {
            is ProjectMatch.One -> {
                projectId = match.project.id
                projectName = match.project.name
            }
            is ProjectMatch.Several -> return AddResult(
                ok = false,
                notice = "The name #${match.name} matches ${match.candidates.joinToString(", ")}. " +
                    "Write the full name.",
            )
            is ProjectMatch.None ->
                notice = "No project matches #${match.name}, so the task is in the inbox."
            ProjectMatch.Absent -> {}
        }

        val taskId = Binding.newId("t")
        val tz = if (parsed.time.isNotEmpty()) TimeZone.getDefault().id else null

        val args = buildJsonObject {
            put("id", taskId)
            put("project_id", projectId)
            put("title", title)
            if (parsed.due.isNotEmpty()) put("due_date", parsed.due)
            if (parsed.time.isNotEmpty()) put("due_time", parsed.time)
            if (tz != null) put("due_tz", tz)
            if (parsed.priority != 0) put("priority", parsed.priority)
            if (parsed.rrule.isNotEmpty()) put("rrule", parsed.rrule)
            if (parsed.labels.isNotEmpty()) {
                put("labels", buildJsonArray { parsed.labels.forEach { add(it) } })
            }
        }

        // Priority 4 is the server default, and 1 is the highest. The parser
        // returns 0 for a line that said nothing, so the local row uses 4 to
        // match what the server will store.
        db.tasks().upsertOne(
            TaskEntity(
                id = taskId,
                projectId = projectId,
                sectionId = null,
                assigneeId = null,
                parentId = null,
                orderKey = "m",
                title = title,
                description = "",
                priority = if (parsed.priority != 0) parsed.priority else 4,
                dueDate = parsed.due.ifEmpty { null },
                dueTime = parsed.time.ifEmpty { null },
                dueTz = tz,
                rrule = parsed.rrule.ifEmpty { null },
                rruleFromCompletion = false,
                startDate = null,
                deadline = null,
                durationMin = null,
                state = "open",
                completedAt = null,
                deletedAt = null,
                labels = parsed.labels,
                version = 0,
            )
        )
        queue("task_add", args)
        return AddResult(ok = true, title = title, projectName = projectName, notice = notice)
    }

    /** complete marks a task done at once, then queues the command. */
    suspend fun complete(taskId: String) = changeState(taskId, "task_complete", "done")

    suspend fun uncomplete(taskId: String) = changeState(taskId, "task_uncomplete", "open")

    private suspend fun changeState(taskId: String, type: String, state: String) {
        val task = db.tasks().byId(taskId) ?: return
        db.tasks().upsertOne(task.copy(state = state))
        queue(type, buildJsonObject { put("id", taskId) })
    }

    /**
     * setDue writes a new day onto a list of tasks.
     *
     * One task_update per task, not one command that says "everything
     * overdue". A command names an id and a date, so a replay from the outbox
     * does the same thing tomorrow. A command that carried a query would mean
     * something different every time the server ran it.
     *
     * The whole list goes into the outbox before any network call, so one sync
     * carries every change in one request.
     */
    suspend fun setDue(changes: List<DueChange>) {
        changes.forEach { c ->
            val task = db.tasks().byId(c.id) ?: return@forEach
            db.tasks().upsertOne(task.copy(dueDate = c.dueDate, dueTime = c.dueTime))
            queue(
                "task_update",
                buildJsonObject {
                    put("id", c.id)
                    if (c.dueDate == null) {
                        put("clear", buildJsonArray { add("due_date"); add("due_time") })
                    } else {
                        put("due_date", c.dueDate)
                        // The server keeps a field that a command does not
                        // name, so the time is sent as well. That keeps the
                        // phone and the server reading the same row.
                        if (c.dueTime != null) put("due_time", c.dueTime)
                    }
                },
            )
        }
    }

    /**
     * edit changes one field of one task.
     *
     * The local row changes first and the command goes to the outbox second,
     * so the screen answers a touch with no network. The server keeps every
     * field a command does not name, so each command carries one field only.
     */
    suspend fun edit(taskId: String, change: Edit) {
        val task = db.tasks().byId(taskId) ?: return
        val row: TaskEntity
        val args: JsonObject
        // One command per field, and one field per command. Filing a task is
        // the exception: it moves the project and the section together, so it
        // is a task_move.
        var kind = "task_update"
        when (change) {
            is Edit.Title -> {
                val title = change.value.trim()
                if (title.isEmpty()) return
                row = task.copy(title = title)
                args = buildJsonObject { put("id", taskId); put("title", title) }
            }
            is Edit.Notes -> {
                row = task.copy(description = change.value)
                args = buildJsonObject { put("id", taskId); put("description", change.value) }
            }
            is Edit.Priority -> {
                row = task.copy(priority = change.value)
                args = buildJsonObject { put("id", taskId); put("priority", change.value) }
            }
            is Edit.Project -> {
                row = task.copy(projectId = change.id)
                args = buildJsonObject { put("id", taskId); put("project_id", change.id) }
            }
            is Edit.Labels -> {
                row = task.copy(labels = change.values)
                args = buildJsonObject {
                    put("id", taskId)
                    put("labels", buildJsonArray { change.values.forEach { add(it) } })
                }
            }
            is Edit.Repeat -> {
                row = task.copy(rrule = change.rrule)
                args = field(taskId, "rrule", change.rrule)
            }
            is Edit.Due -> {
                // The time rides with the day, as it does in setDue. A row
                // that holds a time and no day is a row no view can print.
                row = task.copy(dueDate = change.date, dueTime = change.time)
                args = buildJsonObject {
                    put("id", taskId)
                    if (change.date == null) {
                        put("clear", buildJsonArray { add("due_date"); add("due_time") })
                    } else {
                        put("due_date", change.date)
                        if (change.time != null) {
                            put("due_time", change.time)
                        } else {
                            put("clear", buildJsonArray { add("due_time") })
                        }
                    }
                }
            }
            is Edit.Starts -> {
                row = task.copy(startDate = change.date)
                args = field(taskId, "start_date", change.date)
            }
            is Edit.Deadline -> {
                row = task.copy(deadline = change.date)
                args = field(taskId, "deadline", change.date)
            }
            is Edit.Assignee -> {
                row = task.copy(assigneeId = change.accountId)
                // An empty string, not a clear: the server reads
                // assignee_id = "" as nobody, and a clear list is for the
                // fields that are dates.
                args = buildJsonObject {
                    put("id", taskId)
                    put("assignee_id", change.accountId ?: "")
                }
            }
            is Edit.Section -> {
                row = task.copy(sectionId = change.sectionId)
                // task_move carries the project and the section together,
                // because a section belongs to one project and the pair has to
                // agree after the move.
                kind = "task_move"
                args = buildJsonObject {
                    put("id", taskId)
                    put("project_id", task.projectId)
                    put("section_id", change.sectionId ?: "")
                }
            }
        }
        db.tasks().upsertOne(row)
        queue(kind, args)
    }

    /**
     * editEach applies one change per task, in the order given.
     *
     * D-008: a bulk action is many commands, never one command that carries a
     * query. Each pair names its own task, so the outbox can replay the whole
     * set tomorrow and get the same result. The list is a list of pairs and
     * not one change for many ids, because an undo has to put a different old
     * value back on each task.
     *
     * Every write lands before any network call, so one sync carries the whole
     * set in one request.
     */
    suspend fun editEach(changes: List<Pair<String, Edit>>) {
        changes.forEach { (id, change) -> edit(id, change) }
    }

    /** completeMany closes a set of tasks. */
    suspend fun completeMany(ids: List<String>) = ids.forEach { complete(it) }

    /** uncompleteMany reopens a set of tasks, which is how a completion undoes. */
    suspend fun uncompleteMany(ids: List<String>) = ids.forEach { uncomplete(it) }

    suspend fun deleteMany(ids: List<String>) = ids.forEach { delete(it) }

    suspend fun restoreMany(ids: List<String>) = ids.forEach { restore(it) }

    /** field builds a task_update that either sets one field or clears it. */
    private fun field(taskId: String, name: String, value: String?): JsonObject =
        buildJsonObject {
            put("id", taskId)
            if (value == null) {
                put("clear", buildJsonArray { add(name) })
            } else {
                put(name, value)
            }
        }

    /**
     * addSubtask writes one child under a task.
     *
     * The child takes the parent's project. A sub-task in a different project
     * from its parent is a row that shows up in a list where its parent is
     * absent, which reads as an orphan.
     */
    suspend fun addSubtask(parent: TaskEntity, title: String): String? {
        val clean = title.trim()
        if (clean.isEmpty()) return null
        val id = Binding.newId("t")
        db.tasks().upsertOne(
            TaskEntity(
                id = id,
                projectId = parent.projectId,
                sectionId = null,
                assigneeId = null,
                parentId = parent.id,
                orderKey = "m",
                title = clean,
                description = "",
                priority = 4,
                dueDate = null,
                dueTime = null,
                dueTz = null,
                rrule = null,
                rruleFromCompletion = false,
                startDate = null,
                deadline = null,
                durationMin = null,
                state = "open",
                completedAt = null,
                deletedAt = null,
                labels = emptyList(),
                version = 0,
            )
        )
        queue(
            "task_add",
            buildJsonObject {
                put("id", id)
                put("project_id", parent.projectId)
                put("parent_id", parent.id)
                put("title", clean)
            },
        )
        return id
    }

    /**
     * delete hides a task, here and on the server.
     *
     * A delete is a soft delete on both sides: the server sets deleted_at and
     * still returns the row, so the local row does the same. Marking it, and
     * not removing it, is what lets restore put it back with no pull.
     */
    /**
     * addItem puts one item on a shopping list, in the aisle the history
     * suggests.
     *
     * The aisle is a section of the project, so shopping mode needs no schema
     * of its own. See DECISIONS.md D-021. Learning it is one lookup: the
     * newest item of the same name that carries a heading wins, and a person
     * who files it somewhere else teaches the next one.
     *
     * The name is matched without its count and without its case, so "Milk",
     * "milk" and "2x milk" are one item.
     */
    suspend fun addItem(projectId: String, text: String): String? {
        val title = text.trim()
        if (title.isEmpty()) return null
        val id = Binding.newId("t")
        val key = normalItem(title)
        val section = db.tasks().filedInProject(projectId)
            .filter { normalItem(it.title) == key }
            .maxByOrNull { it.completedAt ?: it.id }
            ?.sectionId

        db.tasks().upsertOne(
            TaskEntity(
                id = id, projectId = projectId, sectionId = section, assigneeId = null,
                parentId = null, orderKey = "m", title = title, description = "",
                priority = 4, dueDate = null, dueTime = null, dueTz = null,
                rrule = null, rruleFromCompletion = false, startDate = null,
                deadline = null, durationMin = null, state = "open",
                completedAt = null, deletedAt = null, labels = emptyList(), version = 0,
            )
        )
        queue("task_add", buildJsonObject {
            put("id", id)
            put("project_id", projectId)
            put("title", title)
            if (section != null) put("section_id", section)
        })
        return id
    }

    // --- comments -----------------------------------------------------------
    // A comment is a row with an author, and only the author changes it. The
    // server refuses the rest, so the screen offers the controls to nobody
    // else. See DECISIONS.md D-020.

    /**
     * comment writes one line of talk and queues it.
     *
     * The id is made here, exactly as it is for a task, so the row appears at
     * once and the same uuid reaches the server whenever the network allows.
     */
    suspend fun comment(taskId: String, body: String): String? {
        val text = body.trim()
        if (text.isEmpty()) return null
        val id = Binding.newId("cm")
        db.comments().upsertOne(
            CommentEntity(
                id = id,
                taskId = taskId,
                accountId = settings.accountId,
                body = text,
                createdAt = Instant.now().toString(),
                deletedAt = null,
                version = 0,
            )
        )
        queue("comment_add", buildJsonObject {
            put("id", id)
            put("task_id", taskId)
            put("body", text)
        })
        return id
    }

    suspend fun editComment(id: String, body: String) {
        val text = body.trim()
        if (text.isEmpty()) return
        val row = db.comments().byId(id) ?: return
        db.comments().upsertOne(row.copy(body = text))
        queue("comment_update", buildJsonObject { put("id", id); put("body", text) })
    }

    /**
     * deleteComment hides the line here and asks the server to hide it there.
     *
     * The row leaves this database rather than carrying a stamp, because a
     * deleted comment is never drawn and a pull removes it the same way. See
     * the delta loop in sync().
     */
    suspend fun deleteComment(id: String) {
        db.comments().deleteById(id)
        queue("comment_delete", buildJsonObject { put("id", id) })
    }

    suspend fun delete(taskId: String) {
        val task = db.tasks().byId(taskId) ?: return
        db.tasks().upsertOne(task.copy(deletedAt = Instant.now().toString()))
        queue("task_delete", buildJsonObject { put("id", taskId) })
    }

    /** restore undoes a delete. */
    suspend fun restore(taskId: String) {
        val task = db.tasks().byId(taskId) ?: return
        db.tasks().upsertOne(task.copy(deletedAt = null))
        queue("task_restore", buildJsonObject { put("id", taskId) })
    }

    private suspend fun queue(type: String, args: JsonObject) {
        db.outbox().add(
            OutboxEntity(
                uuid = Binding.newId("c"),
                type = type,
                argsJson = json.encodeToString(JsonObject.serializer(), args),
                createdAt = System.currentTimeMillis(),
            )
        )
    }

    /**
     * undoRefused repairs the local rows behind commands the server refused.
     *
     * A task_add the server refused describes a row that exists nowhere else,
     * and no later pull can remove it, because a pull only writes what the
     * server has. So delete it here.
     *
     * Any other refusal changed a row the server does own, so the truth is one
     * pull away. Drop the high water mark and let the next sync fetch every
     * row again. A refusal is rare, so a full pull is the cheap answer.
     */
    private suspend fun undoRefused(refused: List<OutboxEntity>) {
        var needsFullPull = false
        refused.forEach { cmd ->
            val id = runCatching {
                json.decodeFromString(JsonObject.serializer(), cmd.argsJson)["id"]
                    ?.jsonPrimitive?.content
            }.getOrNull()
            if (cmd.type == "task_add" && id != null) {
                db.tasks().deleteById(id)
            } else {
                needsFullPull = true
            }
        }
        if (needsFullPull) db.meta().put(MetaEntity(KEY_VERSION, "0"))
    }

    // --- sync ---------------------------------------------------------------

    /**
     * sync pushes the outbox and pulls everything above the local version.
     *
     * The push and the pull share one request, so a task that the server
     * accepts comes back in the same answer with its server version.
     */
    suspend fun sync(): SyncResult {
        syncLock.withLock {
            if (!settings.isConfigured) {
                return SyncResult.Failed("Set the server address in settings.", false)
            }
            val pending = db.outbox().oldest(MAX_COMMANDS)
            val since = version()

            // Everything below the network call runs inside the same guard. A
            // bad row in the outbox, or a failing write, must return a failure
            // and not escape: sync() runs in viewModelScope with no handler, so
            // an escaping exception kills the process, and the same row is
            // picked again at the next launch. That is a crash at every start.
            return try {
                val commands = pending.map {
                    CommandDto(
                        uuid = it.uuid,
                        type = it.type,
                        args = json.decodeFromString(JsonObject.serializer(), it.argsJson),
                    )
                }
                val response = api.sync(SyncRequest(since = since, commands = commands))

                // Trust the answer only when it answers THIS request. Every
                // field of SyncResponse has a default and the parser ignores
                // unknown keys, so any JSON object at all decodes to an empty
                // response with version 0. A captive portal that returns
                // {"ok":true} would otherwise look like a clean sync that
                // confirmed nothing, and the code below would empty the outbox
                // and set the version back to zero.
                if (commands.isNotEmpty() && response.applied.isEmpty()) {
                    return SyncResult.Failed(
                        "The server answered without confirming any command. " +
                            "Check the server address.",
                        false,
                    )
                }

                // A fresh start, asked for by the server. A shared list
                // stopped being shared, and a delta cannot describe a row that
                // went away, so the cache goes and the pull below is from
                // zero. The outbox stays: what this phone wrote is still owed.
                if (response.reset) {
                    db.tasks().clear()
                    db.projects().clear()
                    db.sections().clear()
                    db.labels().clear()
                    db.comments().clear()
                }

                // What the pull brings that somebody else did. It has to be
                // read BEFORE the rows are written, because the answer is the
                // difference between the two. A fresh start says nothing: on
                // a first pull every task looks newly assigned.
                val nudges = if (since > 0 && !response.reset) findNudges(response) else emptyList()

                db.projects().upsert(response.projects.map { it.toEntity() })
                db.sections().upsert(response.sections.map { it.toEntity() })
                db.labels().upsert(response.labels.map { it.toEntity() })
                db.tasks().upsert(response.tasks.map { it.toEntity() })
                // A deleted comment leaves this database rather than staying
                // as a tombstone. Nothing here reads one, and a row that is
                // never drawn is a row that only costs space.
                val (goneTalk, liveTalk) = response.comments.partition { it.deletedAt != null }
                goneTalk.forEach { db.comments().deleteById(it.id) }
                db.comments().upsert(liveTalk.map { it.toEntity() })

                // Who is asking, and where their inbox is. The server answers
                // both on every sync, so a phone learns them without a second
                // call and a capture with no project lands in the right place.
                if (response.me.isNotEmpty()) settings.accountId = response.me
                if (response.inbox.isNotEmpty()) settings.inboxId = response.inbox

                // A reset means somebody shared a list or took one back, so
                // the list of people and of shares is out of date as well.
                if (response.reset) readHousehold()

                val answer = response.applied.associateBy { it.uuid }

                // Remove a command ONLY when this answer names it. A command
                // the server never mentioned is a command the server never
                // saw, and the outbox is the one table the server cannot
                // rebuild. Sending it again is free, because the uuid is the
                // primary key here and the server drops a repeat by uuid.
                val settled = pending.filter { answer.containsKey(it.uuid) }
                if (settled.isNotEmpty()) db.outbox().remove(settled.map { it.uuid })

                // A refused command never succeeds on a retry, because the
                // server answer is deterministic. It leaves the outbox with
                // the rest, and the reason reaches the user.
                val refused = pending.filter { answer[it.uuid]?.ok == false }
                val rejected = refused.mapNotNull { answer[it.uuid]?.error }

                db.meta().put(MetaEntity(KEY_VERSION, response.version.toString()))

                // The local write was optimistic, so a refusal leaves a row
                // that disagrees with the server. Undo it, or the phone shows
                // a task for ever that the server has never heard of.
                if (refused.isNotEmpty()) undoRefused(refused)

                SyncResult.Ok(response.version, rejected, nudges)
            } catch (e: ApiError) {
                SyncResult.Failed(e.message ?: "The request failed.", e.unauthorized)
            } catch (e: IOException) {
                SyncResult.Failed(
                    "Cannot reach the server. ${e.message ?: "No answer."}",
                    false,
                )
            } catch (e: Exception) {
                SyncResult.Failed("The sync failed. ${e.message}", false)
            }
        }
    }

    /**
     * findNudges reads what the other person did, out of a delta.
     *
     * It runs before the delta is written, because every answer here is the
     * difference between what arrived and what this database holds.
     *
     * Two facts are worth saying out loud, and they are the two the server
     * sends a Web Push for: a task that is now mine and was not, and a line
     * somebody else wrote. Everything else is a change a person finds when
     * they look.
     *
     * Nothing about my own action is a nudge. Assigning a task to yourself and
     * answering your own comment are both silent, on the phone as on the
     * server.
     */
    private suspend fun findNudges(response: SyncResponse): List<Nudge> {
        val me = settings.accountId
        if (me.isEmpty()) return emptyList()
        val out = mutableListOf<Nudge>()
        val names = db.accounts().allNow().associate { it.id to it.name }

        for (dto in response.tasks) {
            if (dto.assigneeId != me || dto.deletedAt != null) continue
            val before = db.tasks().byId(dto.id)
            // A task that is new to this phone AND assigned to me is a nudge
            // as well: the other person made it and gave it away in one write.
            if (before != null && before.assigneeId == me) continue
            out += Nudge(
                kind = Nudge.ASSIGNED,
                taskId = dto.id,
                title = dto.title,
                // The server does not say who wrote the change, so the name is
                // the one other person in a household of two, and nothing at
                // all in a bigger one. See docs/BACKLOG.md.
                who = names.filterKeys { it != me }.values.singleOrNull().orEmpty(),
                body = "",
            )
        }

        for (dto in response.comments) {
            if (dto.accountId == me || dto.deletedAt != null) continue
            // A line this phone already holds is a line it already showed.
            if (db.comments().byId(dto.id) != null) continue
            val task = db.tasks().byId(dto.taskId)
            out += Nudge(
                kind = Nudge.COMMENTED,
                taskId = dto.taskId,
                title = task?.title ?: response.tasks.firstOrNull { it.id == dto.taskId }?.title.orEmpty(),
                who = names[dto.accountId].orEmpty(),
                body = dto.body,
            )
        }
        return out
    }

    /**
     * join redeems an invitation code and makes this phone a second account.
     *
     * The code is the credential: this is the one call that carries no device
     * token, and the answer carries the token that every later call does.
     *
     * The cache goes, because what it holds belongs to whoever the phone was
     * before. The outbox goes with it, and it is the only place in this class
     * that drops it: a command written by the old account would land in the
     * new account's inbox, which is worse than losing it. A phone that is
     * joining is a phone with nothing owed.
     */
    suspend fun join(code: String, name: String): SyncResult {
        if (!settings.isConfigured) {
            return SyncResult.Failed("Set the server address first.", false)
        }
        return try {
            val answer = api.join(code, name)
            if (answer.token.isEmpty()) {
                return SyncResult.Failed("The server did not answer with a token.", false)
            }
            reset()
            db.outbox().clear()
            settings.token = answer.token
            settings.accountId = answer.account
            settings.accountName = answer.name
            sync()
        } catch (e: ApiError) {
            SyncResult.Failed(e.message ?: "The invitation was refused.", false)
        } catch (e: IOException) {
            SyncResult.Failed("Cannot reach the server. ${e.message ?: "No answer."}", false)
        } catch (e: Exception) {
            SyncResult.Failed("Joining failed. ${e.message}", false)
        }
    }

    /**
     * readHousehold reads who is in the house and keeps the list.
     *
     * It fails quietly. A household of one needs none of it, and a phone with
     * no network keeps the list it had.
     */
    suspend fun readHousehold() {
        try {
            val answer = api.household()
            if (answer.me.isNotEmpty()) settings.accountId = answer.me
            if (answer.inbox.isNotEmpty()) settings.inboxId = answer.inbox
            db.accounts().clear()
            db.accounts().upsert(
                answer.people.map {
                    AccountEntity(id = it.id, name = it.name, isMe = it.isMe, isOwner = it.isOwner)
                }
            )
            answer.people.firstOrNull { it.isMe }?.let { settings.accountName = it.name }
        } catch (e: Exception) {
            // A server that is older than the household answers 404 here, and
            // that is not a failure the user has to read.
        }
    }

    /**
     * reset drops the cache and the high water mark, so the next sync pulls
     * everything again.
     *
     * The outbox stays. It is the only table the server cannot rebuild, and a
     * user who clears a cache does not expect to lose an unsent capture.
     */
    suspend fun reset() {
        db.tasks().clear()
        db.projects().clear()
        db.sections().clear()
        db.labels().clear()
        db.comments().clear()
        db.meta().clear()
    }

    private companion object {
        const val KEY_VERSION = "sync_version"
        const val MAX_COMMANDS = 200
        // Only used before the first sync answers. internal/store names the
        // inbox row "inbox", and is_inbox replaces this as soon as a pull lands.
        const val FALLBACK_INBOX = "inbox"
    }
}

/**
 * QTY reads a count in front of an item: "2x milk", "2 × milk". The x is
 * required, so "20 minutes of stretching" is a task and not twenty minutes.
 * internal/webui/assets/app.js holds the same pattern.
 */
private val QTY = Regex("""^(\d{1,3})\s*[x\u00d7]\s*(.+)$""", RegexOption.IGNORE_CASE)

/** itemCount and itemName split "2x milk" into the chip and the words. */
fun itemCount(title: String): String = QTY.find(title.trim())?.groupValues?.get(1).orEmpty()

fun itemName(title: String): String =
    QTY.find(title.trim())?.groupValues?.get(2)?.trim() ?: title.trim()

/** normalItem is the key that "Milk", "milk" and "2x milk" share. */
fun normalItem(title: String): String =
    itemName(title).lowercase().replace(Regex("""\s+"""), " ")

private fun ProjectDto.toEntity() = ProjectEntity(
    id = id, name = name, color = color, parentId = parentId,
    orderKey = orderKey, isInbox = isInbox, deletedAt = deletedAt, version = version,
)

private fun SectionDto.toEntity() = SectionEntity(
    id = id, projectId = projectId, name = name, orderKey = orderKey,
    deletedAt = deletedAt, version = version,
)

private fun CommentDto.toEntity() = CommentEntity(
    id = id, taskId = taskId, accountId = accountId, body = body,
    createdAt = createdAt, deletedAt = deletedAt, version = version,
)

private fun LabelDto.toEntity() = LabelEntity(
    id = id, name = name, color = color, deletedAt = deletedAt, version = version,
)

private fun TaskDto.toEntity() = TaskEntity(
    id = id, projectId = projectId, sectionId = sectionId, assigneeId = assigneeId,
    parentId = parentId, orderKey = orderKey,
    title = title, description = description, priority = priority,
    dueDate = dueDate, dueTime = dueTime, dueTz = dueTz,
    rrule = rrule, rruleFromCompletion = rruleFromCompletion,
    startDate = startDate, deadline = deadline, durationMin = durationMin,
    state = state, completedAt = completedAt, deletedAt = deletedAt,
    labels = labels, version = version,
)
