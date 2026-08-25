// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.data.db

import androidx.room.Entity
import androidx.room.PrimaryKey
import androidx.room.TypeConverter

@Entity(tableName = "tasks")
data class TaskEntity(
    @PrimaryKey val id: String,
    val projectId: String,
    val parentId: String?,
    val orderKey: String,
    val title: String,
    val description: String,
    val priority: Int,
    val dueDate: String?,
    val dueTime: String?,
    val dueTz: String?,
    val rrule: String?,
    val rruleFromCompletion: Boolean,
    val startDate: String?,
    val deadline: String?,
    val durationMin: Int?,
    val state: String,
    val completedAt: String?,
    val deletedAt: String?,
    val labels: List<String>,
    val version: Long,
)

@Entity(tableName = "projects")
data class ProjectEntity(
    @PrimaryKey val id: String,
    val name: String,
    val color: String,
    val parentId: String?,
    val orderKey: String,
    val isInbox: Boolean,
    val deletedAt: String?,
    val version: Long,
)

@Entity(tableName = "labels")
data class LabelEntity(
    @PrimaryKey val id: String,
    val name: String,
    val color: String,
    val deletedAt: String?,
    val version: Long,
)

/**
 * OutboxEntity is a command that the server has not confirmed.
 *
 * D-004 says that the outbox is the only client state the server cannot
 * rebuild. Every other table is a cache of the command log. Therefore this
 * table drains as soon as the network allows, and it stays small.
 *
 * The uuid is the primary key, so a resend of the same row is the same
 * command, and the server skips it.
 */
@Entity(tableName = "outbox")
data class OutboxEntity(
    @PrimaryKey val uuid: String,
    val type: String,
    val argsJson: String,
    val createdAt: Long,
)

/** MetaEntity holds the sync high water mark and the last error. */
@Entity(tableName = "meta")
data class MetaEntity(
    @PrimaryKey val key: String,
    val value: String,
)

class Converters {
    @TypeConverter
    fun labelsToString(v: List<String>): String = v.joinToString(",")

    @TypeConverter
    fun stringToLabels(v: String): List<String> =
        if (v.isEmpty()) emptyList() else v.split(",")
}
