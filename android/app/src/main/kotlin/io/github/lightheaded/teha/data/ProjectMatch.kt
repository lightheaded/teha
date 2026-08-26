// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.data

import io.github.lightheaded.teha.data.db.ProjectEntity

/**
 * The answer to a `#Name` in a quick add line.
 *
 * The server rejects a `project` name that matches nothing, and it drops the
 * whole command with it. Every client therefore resolves the name itself and
 * sends `project_id`. See matchProject in cmd/teha/client.go, which this
 * mirrors line for line.
 */
sealed interface ProjectMatch {
    /** The line named no project. */
    data object Absent : ProjectMatch

    data class One(val project: ProjectEntity) : ProjectMatch

    /** No project matches. The task goes to the inbox and the user hears why. */
    data class None(val name: String) : ProjectMatch

    /** Two or more names share the prefix. Nothing is written. */
    data class Several(val name: String, val candidates: List<String>) : ProjectMatch
}

/**
 * matchProject takes an exact name first, then a prefix that only one project
 * has. An ambiguous prefix returns every candidate, so the user can type more.
 */
fun matchProject(projects: List<ProjectEntity>, name: String): ProjectMatch {
    if (name.isEmpty()) return ProjectMatch.Absent
    val low = name.lowercase()
    projects.firstOrNull { it.name.lowercase() == low }?.let { return ProjectMatch.One(it) }
    val hits = projects.filter { it.name.lowercase().startsWith(low) }
    return when {
        hits.size == 1 -> ProjectMatch.One(hits[0])
        hits.isEmpty() -> ProjectMatch.None(name)
        else -> ProjectMatch.Several(name, hits.map { it.name })
    }
}
