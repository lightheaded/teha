// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.data

import android.content.Context
import io.github.lightheaded.teha.data.db.LabelEntity
import io.github.lightheaded.teha.data.db.MetaEntity
import io.github.lightheaded.teha.data.db.OutboxEntity
import io.github.lightheaded.teha.data.db.ProjectEntity
import io.github.lightheaded.teha.data.db.TaskEntity
import io.github.lightheaded.teha.data.db.TehaDatabase
import io.github.lightheaded.teha.data.net.ApiClient
import io.github.lightheaded.teha.data.net.ApiError
import io.github.lightheaded.teha.data.net.CommandDto
import io.github.lightheaded.teha.data.net.LabelDto
import io.github.lightheaded.teha.data.net.ProjectDto
import io.github.lightheaded.teha.data.net.SyncRequest
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

/** The result of one sync. */
sealed interface SyncResult {
    data class Ok(val version: Long, val rejected: List<String>) : SyncResult
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
    val labels: Flow<List<LabelEntity>> = db.labels().all()
    val outboxCount: Flow<Int> = db.outbox().countFlow()

    fun todayTasks(today: String): Flow<List<TaskEntity>> = db.tasks().today(today)
    fun openTasks(): Flow<List<TaskEntity>> = db.tasks().allOpen()

    suspend fun projectsNow(): List<ProjectEntity> = db.projects().allNow()

    /** inboxId reads the project the server marks as the inbox. */
    suspend fun inboxId(): String =
        db.projects().allNow().firstOrNull { it.isInbox }?.id ?: FALLBACK_INBOX

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

                db.projects().upsert(response.projects.map { it.toEntity() })
                db.labels().upsert(response.labels.map { it.toEntity() })
                db.tasks().upsert(response.tasks.map { it.toEntity() })

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

                SyncResult.Ok(response.version, rejected)
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
     * reset drops the cache and the high water mark, so the next sync pulls
     * everything again.
     *
     * The outbox stays. It is the only table the server cannot rebuild, and a
     * user who clears a cache does not expect to lose an unsent capture.
     */
    suspend fun reset() {
        db.tasks().clear()
        db.projects().clear()
        db.labels().clear()
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

private fun ProjectDto.toEntity() = ProjectEntity(
    id = id, name = name, color = color, parentId = parentId,
    orderKey = orderKey, isInbox = isInbox, deletedAt = deletedAt, version = version,
)

private fun LabelDto.toEntity() = LabelEntity(
    id = id, name = name, color = color, deletedAt = deletedAt, version = version,
)

private fun TaskDto.toEntity() = TaskEntity(
    id = id, projectId = projectId, parentId = parentId, orderKey = orderKey,
    title = title, description = description, priority = priority,
    dueDate = dueDate, dueTime = dueTime, dueTz = dueTz,
    rrule = rrule, rruleFromCompletion = rruleFromCompletion,
    startDate = startDate, deadline = deadline, durationMin = durationMin,
    state = state, completedAt = completedAt, deletedAt = deletedAt,
    labels = labels, version = version,
)
