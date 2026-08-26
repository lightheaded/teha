// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CheckboxDefaults
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.lightheaded.teha.data.db.ProjectEntity
import io.github.lightheaded.teha.data.db.TaskEntity
import io.github.lightheaded.teha.ui.theme.priorityColor
import java.time.DayOfWeek
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.time.format.TextStyle
import java.time.temporal.TemporalAdjusters
import java.util.Locale

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TaskListScreen(vm: TehaViewModel, onOpenSettings: () -> Unit) {
    val state by vm.state.collectAsStateWithLifecycle()
    val tasks by vm.tasks.collectAsStateWithLifecycle()
    val projects by vm.projects.collectAsStateWithLifecycle()
    val today by vm.todayIso.collectAsStateWithLifecycle()
    val overdue by vm.overdue.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }

    LaunchedEffect(state.message) {
        val message = state.message ?: return@LaunchedEffect
        val answer = snackbar.showSnackbar(message, actionLabel = state.undoLabel)
        if (answer == SnackbarResult.ActionPerformed) vm.undoReschedule()
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
            // imePadding, because MainActivity calls enableEdgeToEdge(). That
            // sets decorFitsSystemWindows to false, so adjustResize in the
            // manifest no longer moves the window, and Scaffold's default
            // insets cover the system bars and not the keyboard. Without this
            // the keyboard opens straight over the field and the user types
            // blind. QuickAddActivity already does the same.
            Surface(tonalElevation = 3.dp, modifier = Modifier.imePadding()) {
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
            if (overdue.isNotEmpty()) {
                OverdueBar(
                    count = overdue.size,
                    today = today,
                    onPick = { vm.rescheduleOverdue(it) },
                )
            }
            PullToRefreshBox(
                isRefreshing = state.syncing,
                // announce = true: a person asked, so the result gets a
                // sentence and the spinner stays visible long enough to see.
                onRefresh = { vm.sync(announce = true) },
                modifier = Modifier.fillMaxSize(),
            ) {
                if (tasks.isEmpty()) {
                    // A LazyColumn, not a Box, even for one line of text.
                    // PullToRefreshBox reads the pull through nested scroll, and
                    // only a scrollable child sends those events. With a plain
                    // Box the gesture did nothing at all: no spinner, no sync,
                    // no feedback. The empty screen is exactly where a person
                    // first tries to pull, so it was the worst place to lose it.
                    LazyColumn(modifier = Modifier.fillMaxSize()) {
                        item {
                            Box(
                                modifier = Modifier.fillParentMaxSize(),
                                contentAlignment = Alignment.Center,
                            ) {
                                Text(
                                    if (state.configured) "Nothing due. Pull down to sync."
                                    else "Set the server address in settings.",
                                    style = MaterialTheme.typography.bodyMedium,
                                )
                            }
                        }
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

/**
 * OverdueBar says how many tasks are late and moves them all in one touch.
 *
 * The morning after a busy week a dozen tasks all say yesterday, and the only
 * other way out is a dozen separate edits. Todoist puts the same button on the
 * overdue section for the same reason.
 *
 * onPick receives an ISO day, or null for "no date".
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun OverdueBar(count: Int, today: String, onPick: (String?) -> Unit) {
    var open by remember { mutableStateOf(false) }
    val day = runCatching { LocalDate.parse(today) }.getOrNull() ?: LocalDate.now()
    // The same five choices as the web client, and each one shows the day it
    // means. A label with no date behind it makes the user guess.
    val choices: List<Pair<String, LocalDate?>> = listOf(
        "Today" to day,
        "Tomorrow" to day.plusDays(1),
        "This weekend" to day.with(TemporalAdjusters.nextOrSame(DayOfWeek.SATURDAY)),
        // next, not nextOrSame: on a Monday "next week" means the Monday after
        // this one, and a choice that resolves to today is already above.
        "Next week" to day.with(TemporalAdjusters.next(DayOfWeek.MONDAY)),
        "No date" to null,
    )
    val dayFormat = remember { DateTimeFormatter.ofPattern("EEE d MMM", Locale.getDefault()) }

    Surface(color = MaterialTheme.colorScheme.surfaceVariant) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(start = 16.dp, end = 8.dp, top = 2.dp, bottom = 2.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                if (count == 1) "1 overdue" else "$count overdue",
                style = MaterialTheme.typography.labelLarge,
                color = priorityColor(1),
                modifier = Modifier.weight(1f),
            )
            Box {
                TextButton(onClick = { open = true }) { Text("Reschedule") }
                DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
                    choices.forEach { (label, target) ->
                        DropdownMenuItem(
                            text = { Text(label) },
                            trailingIcon = if (target != null) {
                                {
                                    Text(
                                        target.format(dayFormat),
                                        style = MaterialTheme.typography.labelMedium,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    )
                                }
                            } else {
                                null
                            },
                            onClick = {
                                open = false
                                onPick(target?.toString())
                            },
                        )
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
