// SPDX-License-Identifier: Apache-2.0

// The wire shapes of POST /v1/sync. Every field name mirrors internal/store
// exactly. A rename on the server is a break here, so the names stay literal.
package io.github.lightheaded.teha.data.net

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

@Serializable
data class ProjectDto(
    val id: String,
    val name: String = "",
    val color: String = "",
    @SerialName("parent_id") val parentId: String? = null,
    @SerialName("order_key") val orderKey: String = "",
    @SerialName("is_inbox") val isInbox: Boolean = false,
    @SerialName("deleted_at") val deletedAt: String? = null,
    @SerialName("v") val version: Long = 0,
)

@Serializable
data class SectionDto(
    val id: String,
    @SerialName("project_id") val projectId: String = "",
    val name: String = "",
    @SerialName("order_key") val orderKey: String = "",
    @SerialName("deleted_at") val deletedAt: String? = null,
    @SerialName("v") val version: Long = 0,
)

@Serializable
data class LabelDto(
    val id: String,
    val name: String = "",
    val color: String = "",
    @SerialName("deleted_at") val deletedAt: String? = null,
    @SerialName("v") val version: Long = 0,
)

@Serializable
data class TaskDto(
    val id: String,
    @SerialName("project_id") val projectId: String = "inbox",
    @SerialName("section_id") val sectionId: String? = null,
    @SerialName("assignee_id") val assigneeId: String? = null,
    @SerialName("parent_id") val parentId: String? = null,
    @SerialName("order_key") val orderKey: String = "",
    val title: String = "",
    val description: String = "",
    val priority: Int = 0,
    @SerialName("due_date") val dueDate: String? = null,
    @SerialName("due_time") val dueTime: String? = null,
    @SerialName("due_tz") val dueTz: String? = null,
    val rrule: String? = null,
    @SerialName("rrule_from_completion") val rruleFromCompletion: Boolean = false,
    @SerialName("start_date") val startDate: String? = null,
    val deadline: String? = null,
    @SerialName("duration_min") val durationMin: Int? = null,
    val state: String = "open",
    @SerialName("completed_at") val completedAt: String? = null,
    @SerialName("deleted_at") val deletedAt: String? = null,
    @SerialName("source_ref") val sourceRef: String? = null,
    val labels: List<String> = emptyList(),
    @SerialName("v") val version: Long = 0,
)

@Serializable
data class CommentDto(
    val id: String,
    @SerialName("task_id") val taskId: String = "",
    @SerialName("account_id") val accountId: String = "",
    val body: String = "",
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("deleted_at") val deletedAt: String? = null,
    @SerialName("v") val version: Long = 0,
)

@Serializable
data class CommandDto(val uuid: String, val type: String, val args: JsonObject)

@Serializable
data class SyncRequest(val since: Long, val commands: List<CommandDto> = emptyList())

@Serializable
data class AppliedDto(
    val uuid: String = "",
    val ok: Boolean = false,
    val id: String = "",
    val error: String = "",
)

@Serializable
data class SyncResponse(
    val version: Long = 0,
    val applied: List<AppliedDto> = emptyList(),
    val projects: List<ProjectDto> = emptyList(),
    val sections: List<SectionDto> = emptyList(),
    val labels: List<LabelDto> = emptyList(),
    val tasks: List<TaskDto> = emptyList(),
    val comments: List<CommentDto> = emptyList(),
    // reset means "throw away what you hold and pull from zero". It arrives
    // when a shared list stops being shared: a scoped pull cannot report that
    // a row went away, so the server says so once. See store.Delta.
    val reset: Boolean = false,
    // inbox and me name the account that is asking. Each account has its own
    // inbox, so a capture with no project must not assume the fixed id.
    val inbox: String = "",
    val me: String = "",
)

/** The request and the answer of POST /v1/join. It carries no token. */
@Serializable
data class JoinRequest(val code: String, val name: String)

@Serializable
data class JoinResponse(
    val ok: Boolean = false,
    // The device token, shown once. Every later request carries it.
    val token: String = "",
    val account: String = "",
    val name: String = "",
)

/** One person in the household, from GET /v1/household. */
@Serializable
data class PersonDto(
    val id: String,
    val name: String = "",
    @SerialName("is_me") val isMe: Boolean = false,
    @SerialName("is_owner") val isOwner: Boolean = false,
)

@Serializable
data class HouseholdResponse(
    val me: String = "",
    val inbox: String = "",
    val people: List<PersonDto> = emptyList(),
)

@Serializable
data class HealthResponse(val ok: Boolean = false, val version: Long = 0)
