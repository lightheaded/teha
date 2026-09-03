// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.ui

import android.text.format.DateFormat
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.clickable
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Send
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CheckboxDefaults
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TimePicker
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.material3.rememberTimePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.dp
import io.github.lightheaded.teha.data.Edit
import io.github.lightheaded.teha.data.db.AccountEntity
import io.github.lightheaded.teha.data.db.CommentEntity
import io.github.lightheaded.teha.data.db.ProjectEntity
import io.github.lightheaded.teha.data.db.SectionEntity
import io.github.lightheaded.teha.data.db.TaskEntity
import io.github.lightheaded.teha.parser.Binding
import io.github.lightheaded.teha.ui.theme.priorityColor
import kotlinx.coroutines.delay
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset

/**
 * TaskDetailSheet edits every field one task has.
 *
 * Until this screen existed a phone row could only be ticked off, and every
 * other change needed the browser. That was the largest daily gap between the
 * two clients.
 *
 * Each change goes straight into the local row and the outbox, so the screen
 * never waits for the server. The sheet closes with one sync, which carries
 * every change made while it was open in one request.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun TaskDetailSheet(
    task: TaskEntity,
    subtasks: List<TaskEntity>,
    projects: List<ProjectEntity>,
    // The sections of every project. The chip shows the ones of this task's
    // project, and it shows nothing at all when that project has none.
    sections: List<SectionEntity>,
    // The people of the household, and who this phone is. A list of one person
    // draws no assignee chip: a field that always says "me" is in the way.
    people: List<AccountEntity>,
    me: String,
    // The talk on this task, oldest first. It is a conversation between two
    // people, so a list of one person still draws it: a note to yourself on a
    // task is what the browser has always allowed.
    comments: List<CommentEntity>,
    knownLabels: List<String>,
    today: String,
    onEdit: (Edit) -> Unit,
    onToggleTask: (TaskEntity) -> Unit,
    onAddSubtask: (String) -> Unit,
    onComment: (String) -> Unit,
    onEditComment: (String, String) -> Unit,
    onDeleteComment: (String) -> Unit,
    onDelete: () -> Unit,
    onClose: () -> Unit,
) {
    val sheet = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val project = projects.firstOrNull { it.id == task.projectId }

    ModalBottomSheet(onDismissRequest = onClose, sheetState = sheet) {
        Column(
            modifier = Modifier
                .verticalScroll(rememberScrollState())
                .imePadding()
                .navigationBarsPadding()
                .padding(horizontal = 16.dp)
                .padding(bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            TitleAndNotes(task, onEdit)

            FlowRow(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                DayChip(
                    name = "Due",
                    value = task.dueDate,
                    today = today,
                    onPick = { onEdit(Edit.Due(it, if (it == null) null else task.dueTime)) },
                )
                // The time only makes sense with a day. The server accepts a
                // row that holds a time and no day, and no view can print one.
                if (task.dueDate != null) {
                    TimeChip(
                        value = task.dueTime,
                        onPick = { onEdit(Edit.Due(task.dueDate, it)) },
                    )
                }
                PriorityChip(task.priority, onEdit)
                ProjectChip(project, projects, onEdit)
                SectionChip(task, sections, onEdit)
                AssigneeChip(task, people, me, onEdit)
                LabelsChip(task.labels, knownLabels, onEdit)
                RepeatChip(task.rrule, onEdit)
                DayChip(
                    name = "Starts",
                    value = task.startDate,
                    today = today,
                    onPick = { onEdit(Edit.Starts(it)) },
                )
                DayChip(
                    name = "Deadline",
                    value = task.deadline,
                    today = today,
                    onPick = { onEdit(Edit.Deadline(it)) },
                )
            }

            HorizontalDivider()
            Subtasks(subtasks, onToggleTask, onAddSubtask)
            HorizontalDivider()
            Talk(comments, people, me, onComment, onEditComment, onDeleteComment)
            HorizontalDivider()

            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                TextButton(
                    onClick = onDelete,
                    colors = ButtonDefaults.textButtonColors(
                        contentColor = MaterialTheme.colorScheme.error,
                    ),
                ) {
                    Icon(Icons.Filled.Delete, contentDescription = null)
                    Spacer(Modifier.width(6.dp))
                    Text("Delete", fontWeight = FontWeight.Medium)
                }
                Text(
                    "Changes save as you type.",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = 8.dp),
                )
            }
        }
    }
}

/**
 * TitleAndNotes holds the two free text fields.
 *
 * Each field keeps its own copy of the text and writes it half a second after
 * the typing stops. The copy is seeded from the task id and not from the title,
 * because a field that follows the database moves the cursor under the user's
 * finger every time a sync lands.
 */
@Composable
private fun TitleAndNotes(task: TaskEntity, onEdit: (Edit) -> Unit) {
    var title by remember(task.id) { mutableStateOf(task.title) }
    var notes by remember(task.id) { mutableStateOf(task.description) }

    LaunchedEffect(title) {
        if (title.trim() == task.title || title.isBlank()) return@LaunchedEffect
        delay(COMMIT_DELAY_MS)
        onEdit(Edit.Title(title))
    }
    LaunchedEffect(notes) {
        if (notes == task.description) return@LaunchedEffect
        delay(COMMIT_DELAY_MS)
        onEdit(Edit.Notes(notes))
    }

    OutlinedTextField(
        value = title,
        onValueChange = { title = it },
        label = { Text("Title") },
        textStyle = MaterialTheme.typography.titleMedium,
        modifier = Modifier.fillMaxWidth(),
    )
    OutlinedTextField(
        value = notes,
        onValueChange = { notes = it },
        label = { Text("Notes") },
        minLines = 2,
        modifier = Modifier.fillMaxWidth(),
    )
}

/**
 * DayChip shows one date field and changes it.
 *
 * The menu offers the same five presets as the overdue button, and one more
 * entry that opens a calendar. A preset covers almost every choice a person
 * makes, and the calendar covers the rest.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun DayChip(name: String, value: String?, today: String, onPick: (String?) -> Unit) {
    var open by remember { mutableStateOf(false) }
    var calendar by remember { mutableStateOf(false) }
    val now = parseDay(today) ?: LocalDate.now()

    Box {
        AssistChip(
            onClick = { open = true },
            label = {
                Text(if (value == null) name else "$name ${dueLabel(value, null, today)}")
            },
            colors = if (isOverdue(value, today)) {
                AssistChipDefaults.assistChipColors(labelColor = priorityColor(1))
            } else {
                AssistChipDefaults.assistChipColors()
            },
        )
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            dayChoices(now).forEach { (label, target) ->
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
            DropdownMenuItem(
                text = { Text("Pick a day…") },
                onClick = { open = false; calendar = true },
            )
        }
    }

    if (calendar) {
        val state = rememberDatePickerState(
            initialSelectedDateMillis = (parseDay(value) ?: now).toEpochDay() * MILLIS_PER_DAY,
        )
        DatePickerDialog(
            onDismissRequest = { calendar = false },
            confirmButton = {
                TextButton(onClick = {
                    calendar = false
                    state.selectedDateMillis?.let { onPick(isoFromMillis(it)) }
                }) { Text("Set") }
            },
            dismissButton = {
                TextButton(onClick = { calendar = false }) { Text("Cancel") }
            },
        ) {
            DatePicker(state = state)
        }
    }
}

/** TimeChip sets or clears the time of day. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun TimeChip(value: String?, onPick: (String?) -> Unit) {
    var open by remember { mutableStateOf(false) }
    val context = LocalContext.current
    val hour = value?.take(2)?.toIntOrNull() ?: 9
    val minute = value?.drop(3)?.take(2)?.toIntOrNull() ?: 0

    AssistChip(
        onClick = { open = true },
        label = { Text(value ?: "Time") },
    )

    if (open) {
        val state = rememberTimePickerState(
            initialHour = hour,
            initialMinute = minute,
            is24Hour = DateFormat.is24HourFormat(context),
        )
        AlertDialog(
            onDismissRequest = { open = false },
            confirmButton = {
                TextButton(onClick = {
                    open = false
                    onPick("%02d:%02d".format(state.hour, state.minute))
                }) { Text("Set") }
            },
            dismissButton = {
                // "No time" and Cancel are different answers, and a dialog with
                // only Cancel leaves no way to take a time back off a task.
                Row {
                    TextButton(onClick = { open = false; onPick(null) }) { Text("No time") }
                    TextButton(onClick = { open = false }) { Text("Cancel") }
                }
            },
            text = { TimePicker(state = state) },
        )
    }
}

/** PriorityChip picks p1 to p4. p4 is the default and says nothing. */
@Composable
private fun PriorityChip(priority: Int, onEdit: (Edit) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        AssistChip(
            onClick = { open = true },
            label = { Text(if (priority in 1..3) "p$priority" else "Priority") },
            colors = if (priority in 1..3) {
                AssistChipDefaults.assistChipColors(labelColor = priorityColor(priority))
            } else {
                AssistChipDefaults.assistChipColors()
            },
        )
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            listOf(
                1 to "p1 — urgent",
                2 to "p2 — high",
                3 to "p3 — medium",
                4 to "p4 — none",
            ).forEach { (n, label) ->
                DropdownMenuItem(
                    text = { Text(label, color = priorityColor(n)) },
                    onClick = { open = false; onEdit(Edit.Priority(n)) },
                )
            }
        }
    }
}

/** ProjectChip moves the task. The inbox reads as "Inbox", not as a name. */
@Composable
private fun ProjectChip(
    current: ProjectEntity?,
    projects: List<ProjectEntity>,
    onEdit: (Edit) -> Unit,
) {
    var open by remember { mutableStateOf(false) }
    Box {
        AssistChip(
            onClick = { open = true },
            label = { Text(if (current == null || current.isInbox) "Inbox" else "#${current.name}") },
        )
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            projects.forEach { p ->
                DropdownMenuItem(
                    text = { Text(if (p.isInbox) "Inbox" else p.name) },
                    onClick = { open = false; onEdit(Edit.Project(p.id)) },
                )
            }
        }
    }
}

/**
 * SectionChip files the task under a heading of its project.
 *
 * A project with no section draws no chip. The board in the browser files a
 * task by dragging it, and this is the way that works with one hand.
 */
@Composable
private fun SectionChip(
    task: TaskEntity,
    sections: List<SectionEntity>,
    onEdit: (Edit) -> Unit,
) {
    val mine = sections.filter { it.projectId == task.projectId }
    if (mine.isEmpty()) return
    val current = mine.firstOrNull { it.id == task.sectionId }
    var open by remember { mutableStateOf(false) }
    Box {
        AssistChip(
            onClick = { open = true },
            label = { Text(current?.name ?: "No section") },
        )
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            DropdownMenuItem(
                text = { Text("No section") },
                onClick = { open = false; onEdit(Edit.Section(null)) },
            )
            mine.forEach { section ->
                DropdownMenuItem(
                    text = { Text(section.name) },
                    onClick = { open = false; onEdit(Edit.Section(section.id)) },
                )
            }
        }
    }
}

/**
 * AssigneeChip says who does the task.
 *
 * It draws nothing while the household holds one person. An assignee only
 * means something in a list two people share, and a chip that always says
 * "me" is a chip in the way.
 */
@Composable
private fun AssigneeChip(
    task: TaskEntity,
    people: List<AccountEntity>,
    me: String,
    onEdit: (Edit) -> Unit,
) {
    if (people.size < 2) return
    val current = people.firstOrNull { it.id == task.assigneeId }
    val label = when {
        current == null -> "Nobody"
        current.id == me -> "Me"
        else -> current.name
    }
    var open by remember { mutableStateOf(false) }
    Box {
        AssistChip(onClick = { open = true }, label = { Text(label) })
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            DropdownMenuItem(
                text = { Text("Nobody") },
                onClick = { open = false; onEdit(Edit.Assignee(null)) },
            )
            people.forEach { person ->
                DropdownMenuItem(
                    text = { Text(if (person.id == me) "Me" else person.name) },
                    onClick = { open = false; onEdit(Edit.Assignee(person.id)) },
                )
            }
        }
    }
}

/**
 * LabelsChip edits the label list.
 *
 * The dialog shows every label the account already has, so the common case is
 * two touches and no typing. The field below adds a new one, because a client
 * that can only pick from a list cannot start a new label on the phone.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun LabelsChip(labels: List<String>, known: List<String>, onEdit: (Edit) -> Unit) {
    var open by remember { mutableStateOf(false) }
    AssistChip(
        onClick = { open = true },
        label = { Text(if (labels.isEmpty()) "Labels" else labels.joinToString(" ") { "@$it" }) },
    )
    if (!open) return

    var chosen by remember { mutableStateOf(labels) }
    var fresh by remember { mutableStateOf("") }
    // The order is stable: what the task already carries, then the rest.
    val all = remember(known, labels) { (labels + known).distinct() }

    AlertDialog(
        onDismissRequest = { open = false },
        title = { Text("Labels") },
        confirmButton = {
            TextButton(onClick = {
                open = false
                val extra = fresh.split(",").map { it.trim() }.filter { it.isNotEmpty() }
                onEdit(Edit.Labels((chosen + extra).distinct()))
            }) { Text("Save") }
        },
        dismissButton = { TextButton(onClick = { open = false }) { Text("Cancel") } },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (all.isEmpty()) {
                    Text(
                        "This account has no labels yet. Write one below.",
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    all.forEach { name ->
                        FilterChip(
                            selected = name in chosen,
                            onClick = {
                                chosen = if (name in chosen) chosen - name else chosen + name
                            },
                            label = { Text("@$name") },
                        )
                    }
                }
                OutlinedTextField(
                    value = fresh,
                    onValueChange = { fresh = it },
                    label = { Text("A new label") },
                    singleLine = true,
                )
            }
        },
    )
}

/**
 * RepeatChip sets the recurrence rule.
 *
 * The four presets cover what people actually repeat. The field takes a raw
 * RRULE for anything else, and the shared Go engine judges it: a rule this
 * screen accepted but the server refuses would be a rule the phone shows and
 * the browser does not.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun RepeatChip(rrule: String?, onEdit: (Edit) -> Unit) {
    var open by remember { mutableStateOf(false) }
    AssistChip(
        onClick = { open = true },
        label = { Text(if (rrule == null) "Repeat" else repeatWord(rrule)) },
    )
    if (!open) return

    var text by remember { mutableStateOf(rrule ?: "") }
    val valid = text.isEmpty() || Binding.validRecurrence(text)

    AlertDialog(
        onDismissRequest = { open = false },
        title = { Text("Repeat") },
        confirmButton = {
            TextButton(
                enabled = valid,
                onClick = { open = false; onEdit(Edit.Repeat(text.ifEmpty { null })) },
            ) { Text("Save") }
        },
        dismissButton = { TextButton(onClick = { open = false }) { Text("Cancel") } },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    REPEAT_PRESETS.forEach { (label, rule) ->
                        FilterChip(
                            selected = text == rule,
                            onClick = { text = rule },
                            label = { Text(label) },
                        )
                    }
                }
                OutlinedTextField(
                    value = text,
                    onValueChange = { text = it },
                    label = { Text("RRULE") },
                    placeholder = { Text("FREQ=WEEKLY;BYDAY=MO") },
                    isError = !valid,
                    supportingText = {
                        Text(if (valid) "Empty means it does not repeat." else "This rule does not parse.")
                    },
                    singleLine = true,
                )
            }
        },
    )
}

/** Subtasks lists the children and adds one. */
@Composable
private fun Subtasks(
    subtasks: List<TaskEntity>,
    onToggle: (TaskEntity) -> Unit,
    onAdd: (String) -> Unit,
) {
    var fresh by remember { mutableStateOf("") }
    val stillOpen = subtasks.count { it.state == "open" }
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(
            if (subtasks.isEmpty()) "Sub-tasks" else "Sub-tasks  $stillOpen open",
            style = MaterialTheme.typography.labelLarge,
        )
        subtasks.forEach { kid ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                Checkbox(
                    checked = kid.state != "open",
                    onCheckedChange = { onToggle(kid) },
                    colors = CheckboxDefaults.colors(
                        checkedColor = priorityColor(kid.priority),
                        uncheckedColor = priorityColor(kid.priority),
                    ),
                )
                Text(
                    kid.title,
                    style = MaterialTheme.typography.bodyMedium,
                    // A finished child stays on the list, struck through. A
                    // checklist that hides what is done loses the record of it.
                    textDecoration = if (kid.state == "open") null else TextDecoration.LineThrough,
                )
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            OutlinedTextField(
                value = fresh,
                onValueChange = { fresh = it },
                placeholder = { Text("Add a sub-task") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(onDone = {
                    if (fresh.isNotBlank()) { onAdd(fresh); fresh = "" }
                }),
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = 56.dp),
            )
            IconButton(
                onClick = { if (fresh.isNotBlank()) { onAdd(fresh); fresh = "" } },
                enabled = fresh.isNotBlank(),
            ) {
                Icon(Icons.Filled.Add, contentDescription = "Add the sub-task")
            }
        }
    }
}

/**
 * Talk is the conversation on one task.
 *
 * The author is the only person who may change or remove a line, and the
 * server enforces it, so a line of somebody else's carries no controls at all.
 * A refusal a person can see coming is better than one they read afterwards.
 *
 * A tap on your own line opens it for editing, which is how the browser panel
 * works as well.
 */
@Composable
private fun Talk(
    comments: List<CommentEntity>,
    people: List<AccountEntity>,
    me: String,
    onAdd: (String) -> Unit,
    onEdit: (String, String) -> Unit,
    onDelete: (String) -> Unit,
) {
    var fresh by remember { mutableStateOf("") }
    var editing by remember { mutableStateOf<String?>(null) }

    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(
            if (comments.isEmpty()) "Comments" else "Comments  ${comments.size}",
            style = MaterialTheme.typography.labelLarge,
        )
        comments.forEach { line ->
            val mine = line.accountId == me
            if (editing == line.id) {
                CommentEditor(
                    start = line.body,
                    onDone = { text ->
                        editing = null
                        if (text.isNotBlank() && text.trim() != line.body) onEdit(line.id, text)
                    },
                )
            } else {
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .then(
                            if (mine) Modifier.clickable { editing = line.id } else Modifier
                        )
                        .padding(vertical = 2.dp),
                    verticalArrangement = Arrangement.spacedBy(1.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            if (mine) "me" else personName(line.accountId, people),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        Text(
                            "  ${howLongAgo(line.createdAt)}",
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        if (mine) {
                            Spacer(Modifier.weight(1f))
                            IconButton(onClick = { onDelete(line.id) }) {
                                Icon(
                                    Icons.Filled.Close,
                                    contentDescription = "Delete this comment",
                                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                        }
                    }
                    Text(line.body, style = MaterialTheme.typography.bodyMedium)
                }
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            OutlinedTextField(
                value = fresh,
                onValueChange = { fresh = it },
                placeholder = { Text("Say something") },
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                keyboardActions = KeyboardActions(onSend = {
                    if (fresh.isNotBlank()) { onAdd(fresh); fresh = "" }
                }),
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = 56.dp),
            )
            IconButton(
                onClick = { if (fresh.isNotBlank()) { onAdd(fresh); fresh = "" } },
                enabled = fresh.isNotBlank(),
            ) {
                Icon(Icons.Filled.Send, contentDescription = "Send the comment")
            }
        }
    }
}

/**
 * CommentEditor edits one line in place.
 *
 * It keeps its own copy of the text and writes it when the field loses focus
 * or the keyboard says Done, exactly as the title field does. A field that
 * follows the database moves the cursor under the user's finger on every sync.
 */
@Composable
private fun CommentEditor(start: String, onDone: (String) -> Unit) {
    var text by remember { mutableStateOf(start) }
    OutlinedTextField(
        value = text,
        onValueChange = { text = it },
        label = { Text("Your comment") },
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
        keyboardActions = KeyboardActions(onDone = { onDone(text) }),
        trailingIcon = {
            IconButton(onClick = { onDone(text) }) {
                Icon(Icons.Filled.Check, contentDescription = "Save the comment")
            }
        },
        modifier = Modifier.fillMaxWidth(),
    )
}

/** personName says who somebody is, in one word. */
private fun personName(accountId: String, people: List<AccountEntity>): String =
    people.firstOrNull { it.id == accountId }?.name ?: "someone"

/**
 * howLongAgo says the age of a line in the fewest words.
 *
 * A conversation about a chore is about "an hour ago", never about a
 * timestamp. An unreadable stamp gives an empty string rather than a crash: an
 * import writes whatever Todoist held.
 */
private fun howLongAgo(stamp: String): String {
    val then = runCatching { Instant.parse(stamp) }.getOrNull() ?: return ""
    val mins = (Instant.now().epochSecond - then.epochSecond) / 60
    return when {
        mins < 1L -> "now"
        mins < 60L -> "${mins}m ago"
        mins < 60L * 24 -> "${mins / 60}h ago"
        mins < 60L * 24 * 7 -> "${mins / (60 * 24)}d ago"
        // ofInstant needs API 33 and this app runs from 26, so the zone goes
        // through atZone instead.
        else -> then.atZone(ZoneOffset.UTC).toLocalDate().toString()
    }
}

/** repeatWord prints a rule short enough for a chip. */
private fun repeatWord(rrule: String): String {
    REPEAT_PRESETS.firstOrNull { it.second == rrule }?.let { return it.first }
    val freq = rrule.split(";").firstOrNull { it.startsWith("FREQ=") }?.removePrefix("FREQ=")
    return freq?.lowercase()?.replaceFirstChar { it.uppercase() } ?: "Repeats"
}

private fun isoFromMillis(millis: Long): String =
    Instant.ofEpochMilli(millis).atZone(ZoneOffset.UTC).toLocalDate().toString()

// The date picker reports a day as UTC midnight, so a day is exactly this many
// milliseconds and no time zone enters the sum.
private const val MILLIS_PER_DAY = 86_400_000L

// Long enough that a word is not sent one letter at a time, short enough that
// a person who closes the app at once still keeps what they typed.
private const val COMMIT_DELAY_MS = 500L

private val REPEAT_PRESETS = listOf(
    "Daily" to "FREQ=DAILY",
    "Weekly" to "FREQ=WEEKLY",
    "Monthly" to "FREQ=MONTHLY",
    "Weekdays" to "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR",
)
