// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CheckboxDefaults
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberDrawerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.lightheaded.teha.data.db.AccountEntity
import io.github.lightheaded.teha.data.db.ProjectEntity
import io.github.lightheaded.teha.data.db.TaskEntity
import io.github.lightheaded.teha.ui.theme.priorityColor
import kotlinx.coroutines.launch
import java.time.LocalDate

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TaskListScreen(vm: TehaViewModel, onOpenSettings: () -> Unit) {
    val state by vm.state.collectAsStateWithLifecycle()
    val tasks by vm.tasks.collectAsStateWithLifecycle()
    val projects by vm.projects.collectAsStateWithLifecycle()
    val today by vm.todayIso.collectAsStateWithLifecycle()
    val overdue by vm.overdue.collectAsStateWithLifecycle()
    val detail by vm.detail.collectAsStateWithLifecycle()
    val subtasks by vm.detailSubtasks.collectAsStateWithLifecycle()
    val labels by vm.labels.collectAsStateWithLifecycle()
    val sections by vm.sections.collectAsStateWithLifecycle()
    val people by vm.people.collectAsStateWithLifecycle()
    val marked by vm.marked.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val selecting = marked.isNotEmpty()
    val drawer = rememberDrawerState(DrawerValue.Closed)
    val scope = rememberCoroutineScope()

    // Back leaves the selection before it leaves the screen. A phone user
    // reaches for back to escape a mode, and losing the whole screen instead
    // is the wrong answer.
    BackHandler(enabled = selecting) { vm.clearMarks() }
    // Back closes the drawer for the same reason. ModalNavigationDrawer does
    // not take the key itself.
    BackHandler(enabled = drawer.isOpen && !selecting) { scope.launch { drawer.close() } }

    LaunchedEffect(state.message) {
        val message = state.message ?: return@LaunchedEffect
        val answer = snackbar.showSnackbar(message, actionLabel = state.undoLabel)
        if (answer == SnackbarResult.ActionPerformed) vm.undo()
        vm.dismissMessage()
    }

    ModalNavigationDrawer(
        drawerState = drawer,
        // No swipe while a set is picked. A drag over the list is then a
        // deliberate gesture on the rows, and the drawer must not steal it.
        gesturesEnabled = !selecting,
        drawerContent = {
            ViewDrawerSheet(
                current = state.view,
                projects = projects,
                people = people.size,
                filterError = state.filterError,
                onPick = { view ->
                    vm.setView(view)
                    scope.launch { drawer.close() }
                },
                onFilter = { text ->
                    val accepted = vm.setFilter(text)
                    if (accepted) scope.launch { drawer.close() }
                    accepted
                },
                onTyping = vm::dismissFilterError,
            )
        },
    ) {
        Scaffold(
            snackbarHost = { SnackbarHost(snackbar) },
            topBar = {
                TopAppBar(
                    title = {
                        Column {
                            Text(
                                when {
                                    selecting ->
                                        if (marked.size == 1) "1 selected"
                                        else "${marked.size} selected"
                                    // The title of the view, whatever the view
                                    // is. A typed filter is its own title,
                                    // because a person recognises the words
                                    // they wrote.
                                    else -> state.view.title
                                },
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                            val queued = state.queued
                            if (queued > 0 && !selecting) {
                                Text(
                                    "$queued waiting to send",
                                    style = MaterialTheme.typography.labelSmall,
                                )
                            }
                        }
                    },
                    navigationIcon = {
                        if (selecting) {
                            IconButton(onClick = { vm.clearMarks() }) {
                                Icon(Icons.Filled.Close, contentDescription = "Drop the selection")
                            }
                        } else {
                            IconButton(onClick = { scope.launch { drawer.open() } }) {
                                Icon(Icons.Filled.Menu, contentDescription = "Views and projects")
                            }
                        }
                    },
                    actions = {
                        if (!selecting) {
                            IconButton(onClick = onOpenSettings) {
                                Icon(Icons.Filled.Settings, contentDescription = "Settings")
                            }
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
                    if (selecting) {
                        // The actions take the place of the quick add field. A
                        // phone has room for one bar, and capture is not what the
                        // user is doing while a set is picked.
                        SelectionBar(
                            today = today,
                            projects = projects,
                            onReschedule = vm::bulkReschedule,
                            onPriority = vm::bulkPriority,
                            onProject = vm::bulkProject,
                            onComplete = vm::bulkComplete,
                            onDelete = vm::bulkDelete,
                        )
                    } else {
                        QuickAddBar(parse = vm::parse, onSubmit = { vm.add(it) {} })
                    }
                }
            },
        ) { padding ->
            Column(modifier = Modifier.padding(padding)) {
                // The two chips that used to sit here are gone. Seven views and
                // one row per project do not fit in a row of chips, so the
                // drawer holds them all and the top bar names the one on
                // screen.
                if (overdue.isNotEmpty() && !selecting) {
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
                                        if (state.configured) "Nothing here. Pull down to sync."
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
                                    who = assigneeName(task, people, vm.me()),
                                    today = today,
                                    selected = task.id in marked,
                                    selecting = selecting,
                                    onToggle = { vm.toggle(task) },
                                    onOpen = {
                                        if (selecting) vm.toggleMark(task) else vm.openDetail(task)
                                    },
                                    onLongPress = { vm.toggleMark(task) },
                                )
                                HorizontalDivider()
                            }
                        }
                    }
                }
            }
        }
    }

    // The sheet lives outside the Scaffold content, so it draws over the quick
    // add bar and the snackbar rather than inside the list.
    val openTask = detail
    if (openTask != null) {
        TaskDetailSheet(
            task = openTask,
            subtasks = subtasks,
            projects = projects,
            sections = sections,
            people = people,
            me = vm.me(),
            knownLabels = labels.map { it.name },
            today = today,
            onEdit = vm::edit,
            onToggleTask = vm::toggle,
            onAddSubtask = vm::addSubtask,
            onDelete = vm::deleteOpenTask,
            onClose = vm::onLeave,
        )
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
    // The same five choices as the web client and as the detail screen. They
    // live in Days.kt, so no screen can drift from the others.
    val choices = dayChoices(parseDay(today) ?: LocalDate.now())

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

/**
 * SelectionBar acts on every task the user has picked.
 *
 * The same five actions the browser offers: schedule, priority, move,
 * complete, delete. D-008 holds on both clients, so each one sends one command
 * per task and each one has an undo.
 *
 * The row scrolls sideways rather than dropping an action behind an overflow
 * menu. A narrow phone then shows four of the five, and a nudge reveals the
 * last, which is one gesture instead of two.
 */
@Composable
private fun SelectionBar(
    today: String,
    projects: List<ProjectEntity>,
    onReschedule: (String?) -> Unit,
    onPriority: (Int) -> Unit,
    onProject: (String, String) -> Unit,
    onComplete: () -> Unit,
    onDelete: () -> Unit,
) {
    var days by remember { mutableStateOf(false) }
    var prios by remember { mutableStateOf(false) }
    var where by remember { mutableStateOf(false) }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 4.dp, vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box {
            TextButton(onClick = { days = true }) { Text("Schedule") }
            DropdownMenu(expanded = days, onDismissRequest = { days = false }) {
                dayChoices(parseDay(today) ?: LocalDate.now()).forEach { (label, target) ->
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
                        onClick = { days = false; onReschedule(target?.toString()) },
                    )
                }
            }
        }
        Box {
            TextButton(onClick = { prios = true }) { Text("Priority") }
            DropdownMenu(expanded = prios, onDismissRequest = { prios = false }) {
                listOf(1 to "urgent", 2 to "high", 3 to "medium", 4 to "none").forEach { (n, word) ->
                    DropdownMenuItem(
                        text = { Text("p$n", color = priorityColor(n)) },
                        trailingIcon = {
                            Text(
                                word,
                                style = MaterialTheme.typography.labelMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        },
                        onClick = { prios = false; onPriority(n) },
                    )
                }
            }
        }
        Box {
            TextButton(onClick = { where = true }) { Text("Move") }
            DropdownMenu(expanded = where, onDismissRequest = { where = false }) {
                projects.forEach { p ->
                    val name = if (p.isInbox) "Inbox" else p.name
                    DropdownMenuItem(
                        text = { Text(name) },
                        onClick = {
                            where = false
                            onProject(p.id, if (p.isInbox) "the inbox" else "#${p.name}")
                        },
                    )
                }
            }
        }
        TextButton(onClick = onComplete) { Text("Complete") }
        TextButton(
            onClick = onDelete,
            colors = ButtonDefaults.textButtonColors(
                contentColor = MaterialTheme.colorScheme.error,
            ),
        ) {
            Icon(Icons.Filled.Delete, contentDescription = null)
            Spacer(Modifier.width(4.dp))
            Text("Delete")
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun TaskRow(
    task: TaskEntity,
    project: ProjectEntity?,
    // who is the name to show for the assignee, and it is empty for a task of
    // this person's own: a list that says "me" on every row means nothing.
    who: String,
    today: String,
    selected: Boolean,
    selecting: Boolean,
    onToggle: () -> Unit,
    onOpen: () -> Unit,
    onLongPress: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            // The whole row opens the task, and the checkbox alone completes
            // it. The checkbox sits inside this clickable area, so it declares
            // its own handler and swallows the touch first.
            //
            // A long press starts a selection, which is the gesture every
            // phone gallery and mail app uses for the same job.
            .combinedClickable(onClick = onOpen, onLongClick = onLongPress)
            .background(
                if (selected) MaterialTheme.colorScheme.secondaryContainer
                else MaterialTheme.colorScheme.surface
            )
            .padding(start = 4.dp, end = 12.dp, top = 4.dp, bottom = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // While a set is being picked the box shows membership, not
        // completion. Two meanings for one control in one moment is how a
        // person completes a task they meant to select.
        Checkbox(
            checked = if (selecting) selected else task.state != "open",
            onCheckedChange = { if (selecting) onLongPress() else onToggle() },
            colors = CheckboxDefaults.colors(
                checkedColor = if (selecting) {
                    MaterialTheme.colorScheme.primary
                } else {
                    priorityColor(task.priority)
                },
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
                if (who.isNotEmpty()) add(who)
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

/**
 * assigneeName is the name to print beside a task, and an empty string for a
 * task of this person's own or a household of one.
 */
private fun assigneeName(task: TaskEntity, people: List<AccountEntity>, me: String): String {
    val who = task.assigneeId ?: return ""
    if (who.isEmpty() || who == me || people.size < 2) return ""
    return people.firstOrNull { it.id == who }?.name ?: "someone"
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

