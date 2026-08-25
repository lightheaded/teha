// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CheckboxDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.lightheaded.teha.data.db.ProjectEntity
import io.github.lightheaded.teha.data.db.TaskEntity
import io.github.lightheaded.teha.ui.theme.priorityColor
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.time.format.TextStyle
import java.util.Locale

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TaskListScreen(vm: TehaViewModel, onOpenSettings: () -> Unit) {
    val state by vm.state.collectAsStateWithLifecycle()
    val tasks by vm.tasks.collectAsStateWithLifecycle()
    val projects by vm.projects.collectAsStateWithLifecycle()
    val today by vm.todayIso.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }

    LaunchedEffect(state.message) {
        val message = state.message ?: return@LaunchedEffect
        snackbar.showSnackbar(message)
        vm.dismissMessage()
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(if (state.view == TaskView.TODAY) "Today" else "All open")
                        val queued = state.queued
                        if (queued > 0) {
                            Text(
                                "$queued waiting to send",
                                style = MaterialTheme.typography.labelSmall,
                            )
                        }
                    }
                },
                actions = {
                    IconButton(onClick = onOpenSettings) {
                        Icon(Icons.Filled.Settings, contentDescription = "Settings")
                    }
                },
            )
        },
        bottomBar = {
            Surface(tonalElevation = 3.dp) {
                QuickAddBar(parse = vm::parse, onSubmit = { vm.add(it) {} })
            }
        },
    ) { padding ->
        Column(modifier = Modifier.padding(padding)) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 12.dp, vertical = 4.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                FilterChip(
                    selected = state.view == TaskView.TODAY,
                    onClick = { vm.setView(TaskView.TODAY) },
                    label = { Text("Today") },
                )
                FilterChip(
                    selected = state.view == TaskView.ALL,
                    onClick = { vm.setView(TaskView.ALL) },
                    label = { Text("All open") },
                )
            }
            PullToRefreshBox(
                isRefreshing = state.syncing,
                onRefresh = vm::sync,
                modifier = Modifier.fillMaxSize(),
            ) {
                if (tasks.isEmpty()) {
                    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        Text(
                            if (state.configured) "Nothing due. Pull down to sync."
                            else "Set the server address in settings.",
                            style = MaterialTheme.typography.bodyMedium,
                        )
                    }
                } else {
                    LazyColumn(modifier = Modifier.fillMaxSize()) {
                        items(tasks, key = { it.id }) { task ->
                            TaskRow(
                                task = task,
                                project = projects.firstOrNull { it.id == task.projectId },
                                today = today,
                                onToggle = { vm.toggle(task) },
                            )
                            HorizontalDivider()
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun TaskRow(
    task: TaskEntity,
    project: ProjectEntity?,
    today: String,
    onToggle: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 4.dp, end = 12.dp, top = 4.dp, bottom = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Checkbox(
            checked = task.state != "open",
            onCheckedChange = { onToggle() },
            colors = CheckboxDefaults.colors(
                checkedColor = priorityColor(task.priority),
                uncheckedColor = priorityColor(task.priority),
            ),
        )
        Column(modifier = Modifier.weight(1f)) {
            Text(task.title, style = MaterialTheme.typography.bodyLarge)
            val meta = buildList {
                task.dueDate?.let { add(dueLabel(it, task.dueTime, today)) }
                if (project != null && !project.isInbox) add("#${project.name}")
                task.labels.forEach { add("@$it") }
                if (task.rrule != null) add("repeats")
            }
            if (meta.isNotEmpty()) {
                Text(
                    meta.joinToString("  "),
                    style = MaterialTheme.typography.labelMedium,
                    color = if (isOverdue(task.dueDate, today)) {
                        priorityColor(1)
                    } else {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    },
                    fontWeight = if (isOverdue(task.dueDate, today)) FontWeight.Bold else null,
                )
            }
        }
        PriorityDot(task.priority)
    }
}

@Composable
private fun PriorityDot(priority: Int) {
    // Priority 4 is the default and says nothing, so it gets no mark.
    if (priority >= 4 || priority <= 0) return
    Box(
        modifier = Modifier
            .size(10.dp)
            .clip(CircleShape)
            .background(priorityColor(priority)),
    )
}

private fun isOverdue(due: String?, today: String): Boolean =
    due != null && due < today

/** dueLabel prints a day the way a phone user reads it, not as an ISO string. */
private fun dueLabel(due: String, time: String?, today: String): String {
    val day = runCatching { LocalDate.parse(due) }.getOrNull() ?: return due
    val now = runCatching { LocalDate.parse(today) }.getOrNull() ?: return due
    val name = when {
        day == now -> "today"
        day == now.plusDays(1) -> "tomorrow"
        day == now.minusDays(1) -> "yesterday"
        day.isBefore(now) && day.isAfter(now.minusDays(7)) ->
            day.dayOfWeek.getDisplayName(TextStyle.SHORT, Locale.getDefault())
        day.year == now.year -> day.format(DateTimeFormatter.ofPattern("d MMM"))
        else -> due
    }
    return if (time.isNullOrEmpty()) name else "$name $time"
}
