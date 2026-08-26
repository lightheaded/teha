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
    val labels: List<LabelDto> = emptyList(),
    val tasks: List<TaskDto> = emptyList(),
)

@Serializable
data class HealthResponse(val ok: Boolean = false, val version: Long = 0)
