// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import io.github.lightheaded.teha.TehaApp
import io.github.lightheaded.teha.data.DueChange
import io.github.lightheaded.teha.data.Edit
import io.github.lightheaded.teha.data.SyncResult
import io.github.lightheaded.teha.data.db.LabelEntity
import io.github.lightheaded.teha.data.db.ProjectEntity
import io.github.lightheaded.teha.data.db.TaskEntity
import io.github.lightheaded.teha.parser.Binding
import io.github.lightheaded.teha.parser.ParsedLine
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

/**
 * TaskView is one view of the list: a filter string and the title to show.
 *
 * A query, not an enumeration case. The shared Go compiler turns the string
 * into a WHERE clause over the local Room database, so the phone reaches every
 * view the browser has, every project, and anything a person types. See
 * filter/schema.go for the two dialects of one compiler.
 */
data class TaskView(val query: String, val title: String)

/**
 * BUILT_IN_VIEWS are the views on the phone.
 *
 * The first six are the browser's, in the browser's order. VIEWS in
 * internal/webui/assets/app.js holds the same list, so a change belongs in both
 * files or the two clients drift.
 *
 * All open is the seventh, and the browser has no equal of it. The phone had it
 * before this list existed, and an empty query means every open task, so it
 * stays.
 */
val BUILT_IN_VIEWS = listOf(
    TaskView("today", "Today"),
    TaskView("overdue", "Overdue"),
    TaskView("week", "Next 7 days"),
    TaskView("#inbox", "Inbox"),
    TaskView("no date", "No date"),
    TaskView("p1", "Priority 1"),
    TaskView("", "All open"),
)

/**
 * projectView is the view of one project.
 *
 * The query is the string a person can also type, so the phone and the browser
 * build one view from one name. A name that holds a filter operator (&, |, !, a
 * comma or a parenthesis) reaches no client, because the grammar reads the
 * operator first. docs/BACKLOG.md records that limit.
 */
fun projectView(project: ProjectEntity): TaskView =
    TaskView("#${project.name}", project.name)

data class UiState(
    val view: TaskView = BUILT_IN_VIEWS.first(),
    val syncing: Boolean = false,
    val message: String? = null,
    // filterError holds what the filter compiler said about the last query a
    // person typed. The compiler names the position that failed, so the text
    // goes to the field unchanged.
    val filterError: String? = null,
    // undoLabel is set when the message on screen can be taken back. The
    // snackbar shows it as an action, and a null label means no action.
    val undoLabel: String? = null,
    val queued: Int = 0,
    val configured: Boolean = false,
)

class TehaViewModel(app: Application) : AndroidViewModel(app) {

    private val repo = (app as TehaApp).repository

    private val _state = MutableStateFlow(UiState(configured = repo.settings.isConfigured))
    val state: StateFlow<UiState> = _state.asStateFlow()

    /** The day is read again on every refresh, so the list is right after midnight. */
    private val today = MutableStateFlow(Binding.todayIso())
    val todayIso: StateFlow<String> = today.asStateFlow()

    // The query changes only when the view or the day changes. A message or an
    // outbox count must not restart the database flow.
    //
    // The filter compiles here rather than in the state, because the meaning of
    // "today" moves at midnight. A view holds the words, and the SQL is made
    // again whenever the day changes under it.
    @OptIn(ExperimentalCoroutinesApi::class)
    val tasks: StateFlow<List<TaskEntity>> =
        combine(_state.map { it.view }.distinctUntilChanged(), today) { view, day -> view to day }
            .flatMapLatest { (view, day) ->
                val compiled = Binding.compileFilterRoom(view.query, day)
                // setFilter refuses a bad query before it becomes a view, so
                // this path is for a view that stopped compiling later. An
                // empty list is the honest answer, and never a crash.
                if (compiled.error.isNotEmpty()) flowOf(emptyList<TaskEntity>())
                else repo.tasks(compiled.sql, compiled.argValues)
            }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    // The overdue tasks of the list on screen. The screen, not the whole
    // account: a button must never touch a task the user cannot see.
    val overdue: StateFlow<List<TaskEntity>> =
        combine(tasks, today) { list, day ->
            list.filter { it.dueDate != null && it.dueDate < day }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    val projects: StateFlow<List<ProjectEntity>> = repo.projects
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    val labels: StateFlow<List<LabelEntity>> = repo.labels
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    init {
        viewModelScope.launch {
            repo.outboxCount.collect { n -> _state.value = _state.value.copy(queued = n) }
        }
        if (repo.settings.isConfigured) sync()
    }

    // --- the detail screen ---------------------------------------------------
    //
    // The screen holds an id, not a task. The row it came from is a snapshot,
    // and a sync that lands while the screen is open would leave that snapshot
    // behind. An id plus a database flow always shows what the database holds.

    private val openTaskId = MutableStateFlow<String?>(null)

    @OptIn(ExperimentalCoroutinesApi::class)
    val detail: StateFlow<TaskEntity?> = openTaskId
        .flatMapLatest { id -> if (id == null) flowOf(null) else repo.task(id) }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    @OptIn(ExperimentalCoroutinesApi::class)
    val detailSubtasks: StateFlow<List<TaskEntity>> = openTaskId
        .flatMapLatest { id -> if (id == null) flowOf(emptyList()) else repo.subtasks(id) }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    fun openDetail(task: TaskEntity) {
        openTaskId.value = task.id
    }

    fun closeDetail() {
        openTaskId.value = null
    }

    /**
     * edit changes one field of the open task.
     *
     * No sync call. The detail screen changes several fields in a row, and a
     * sync per keystroke would send one request per character. onLeave() sends
     * the batch when the screen closes, and any other sync carries it earlier.
     */
    fun edit(change: Edit) {
        val id = openTaskId.value ?: return
        viewModelScope.launch { repo.edit(id, change) }
    }

    /** onLeave pushes whatever the detail screen queued. */
    fun onLeave() {
        closeDetail()
        sync()
    }

    fun addSubtask(title: String) {
        val parent = detail.value ?: return
        viewModelScope.launch { repo.addSubtask(parent, title) }
    }

    /** deleteOpenTask hides the task and offers one chance to take it back. */
    fun deleteOpenTask() {
        val task = detail.value ?: return
        closeDetail()
        viewModelScope.launch {
            repo.delete(task.id)
            pendingUndo = {
                repo.restore(task.id)
                "Put \"${task.title}\" back"
            }
            _state.value = _state.value.copy(
                message = "Deleted \"${task.title}\"",
                undoLabel = "Undo",
            )
            sync()
        }
    }

    fun setView(v: TaskView) {
        _state.value = _state.value.copy(view = v, filterError = null)
    }

    /**
     * setFilter takes a query a person typed and makes a view of it.
     *
     * The compiler judges the query first, and a refusal never becomes a view.
     * The message it returns names the position that failed, so it reaches the
     * field unchanged. An empty query means every open task.
     *
     * The title is the query itself. A person who typed the words recognises
     * them, and no shorter name is honest.
     *
     * The answer is true when the query became the view. The caller closes the
     * navigation on a true and leaves it open on a false, so the field that
     * holds the mistake stays on screen.
     */
    fun setFilter(text: String): Boolean {
        val query = text.trim()
        if (query.isEmpty()) {
            setView(BUILT_IN_VIEWS.last())
            return true
        }
        val compiled = Binding.compileFilterRoom(query, today.value)
        if (compiled.error.isNotEmpty()) {
            _state.value = _state.value.copy(filterError = compiled.error)
            return false
        }
        _state.value = _state.value.copy(view = TaskView(query, query), filterError = null)
        return true
    }

    fun dismissFilterError() {
        if (_state.value.filterError != null) {
            _state.value = _state.value.copy(filterError = null)
        }
    }

    fun dismissMessage() {
        _state.value = _state.value.copy(message = null, undoLabel = null)
    }

    // --- a set of tasks ------------------------------------------------------
    //
    // A long press starts a selection and a tap then adds to it, which is what
    // every phone gallery and mail app does. While a selection exists a tap
    // never opens the detail screen, so nobody loses a set by mis-touching one
    // row.

    private val _marked = MutableStateFlow<Set<String>>(emptySet())
    val marked: StateFlow<Set<String>> = _marked.asStateFlow()

    fun toggleMark(task: TaskEntity) {
        val now = _marked.value
        _marked.value = if (task.id in now) now - task.id else now + task.id
    }

    fun clearMarks() {
        _marked.value = emptySet()
    }

    /**
     * markedTasks are the marked tasks that are on the screen.
     *
     * On the screen, not in the account. A mark survives a change of view, and
     * acting on a row the user can no longer see is a change nobody can check.
     */
    private fun markedTasks(): List<TaskEntity> {
        val ids = _marked.value
        return tasks.value.filter { it.id in ids }
    }

    /** bulkPriority sets one priority on the whole set. */
    fun bulkPriority(priority: Int) {
        val set = markedTasks()
        if (set.isEmpty()) return
        val back = set.map { it.id to Edit.Priority(it.priority) as Edit }
        run(
            set = set,
            said = "Set p$priority on",
            work = { repo.editEach(set.map { it.id to Edit.Priority(priority) }) },
            undo = { repo.editEach(back); "Put the priority back on ${countWord(back.size)}" },
        )
    }

    /** bulkProject moves the whole set. */
    fun bulkProject(projectId: String, name: String) {
        val set = markedTasks()
        if (set.isEmpty()) return
        val back = set.map { it.id to Edit.Project(it.projectId) as Edit }
        run(
            set = set,
            said = "Moved",
            tail = " to $name",
            work = { repo.editEach(set.map { it.id to Edit.Project(projectId) }) },
            undo = { repo.editEach(back); "Moved ${countWord(back.size)} back" },
        )
    }

    /** bulkReschedule moves the whole set onto one day, or takes the day away. */
    fun bulkReschedule(date: String?) {
        val set = markedTasks()
        if (set.isEmpty()) return
        val back = set.map { DueChange(it.id, it.dueDate, it.dueTime) }
        run(
            set = set,
            said = if (date == null) "Took the date off" else "Moved",
            tail = if (date == null) "" else " to ${whenWord(date, today.value)}",
            work = {
                repo.setDue(set.map { DueChange(it.id, date, if (date == null) null else it.dueTime) })
            },
            undo = { repo.setDue(back); "Put ${countWord(back.size)} back" },
        )
    }

    fun bulkComplete() {
        val set = markedTasks()
        if (set.isEmpty()) return
        val ids = set.map { it.id }
        run(
            set = set,
            said = "Completed",
            work = { repo.completeMany(ids) },
            undo = { repo.uncompleteMany(ids); "Reopened ${countWord(ids.size)}" },
        )
    }

    fun bulkDelete() {
        val set = markedTasks()
        if (set.isEmpty()) return
        val ids = set.map { it.id }
        run(
            set = set,
            said = "Deleted",
            work = { repo.deleteMany(ids) },
            undo = { repo.restoreMany(ids); "Put ${countWord(ids.size)} back" },
        )
    }

    /**
     * run applies one bulk action, records its undo and says what it did.
     *
     * The undo arrives as an argument rather than being assigned after the
     * call. viewModelScope dispatches on the immediate main dispatcher, so the
     * work below can finish before the caller's next line runs, and an undo
     * assigned on that next line would arrive after the snackbar offered it.
     *
     * The marks drop here, for every action. A set that survived its own
     * action was still marked for the next one, so a second action hit tasks
     * the user believed they had already dealt with.
     */
    private fun run(
        set: List<TaskEntity>,
        said: String,
        tail: String = "",
        work: suspend () -> Unit,
        undo: suspend () -> String,
    ) {
        clearMarks()
        pendingUndo = undo
        viewModelScope.launch {
            work()
            _state.value = _state.value.copy(
                message = "$said ${countWord(set.size)}$tail",
                undoLabel = "Undo",
            )
            sync()
        }
    }

    private fun countWord(n: Int): String = if (n == 1) "1 task" else "$n tasks"

    // --- reschedule ---------------------------------------------------------

    // pendingUndo is the work that takes the last change back, and the
    // sentence to show once it has. It lives outside UiState, because a state
    // object that carries work to do is a state object that runs it twice on a
    // recomposition.
    //
    // A lambda, not a list of changes: a reschedule and a delete both need an
    // undo, and they undo different things. One field here keeps one Undo
    // button on screen, which is what a person expects.
    private var pendingUndo: (suspend () -> String)? = null

    /**
     * rescheduleOverdue moves every overdue task in the list to one day.
     *
     * This is the answer to the morning after a busy week, where a dozen tasks
     * all say "yesterday". date is an ISO day, or null to take the day away.
     */
    fun rescheduleOverdue(date: String?) {
        val late = overdue.value
        if (late.isEmpty()) return
        val back = late.map { DueChange(it.id, it.dueDate, it.dueTime) }
        val n = late.size
        val what = if (n == 1) "1 task" else "$n tasks"
        viewModelScope.launch {
            repo.setDue(late.map { DueChange(it.id, date, if (date == null) null else it.dueTime) })
            pendingUndo = {
                repo.setDue(back)
                if (back.size == 1) "Put 1 task back" else "Put ${back.size} tasks back"
            }
            _state.value = _state.value.copy(
                message = if (date == null) "Took the date off $what" else "Moved $what to ${whenWord(date, today.value)}",
                undoLabel = "Undo",
            )
            sync()
        }
    }

    /** undo takes the last change back, whatever it was. */
    fun undo() {
        val work = pendingUndo ?: return
        pendingUndo = null
        viewModelScope.launch {
            val line = work()
            _state.value = _state.value.copy(message = line, undoLabel = null)
            sync()
        }
    }

    /**
     * sync pulls and pushes.
     *
     * announce is true when a PERSON asked, by pulling the list down. A sync on
     * a local network finishes in tens of milliseconds, so the spinner appears
     * and vanishes before an eye registers it, and the gesture reads as broken.
     * An explicit pull therefore always ends in a sentence. A background sync
     * stays silent, because nobody asked it a question.
     */
    fun sync(announce: Boolean = false) {
        if (_state.value.syncing) return
        _state.value = _state.value.copy(syncing = true)
        viewModelScope.launch {
            today.value = Binding.todayIso()
            val startedAt = System.currentTimeMillis()
            val result = repo.sync()
            // Hold the spinner long enough to be seen. Without this the pull
            // animation snaps back instantly and looks like nothing happened.
            if (announce) {
                val elapsed = System.currentTimeMillis() - startedAt
                if (elapsed < MIN_SPINNER_MS) delay(MIN_SPINNER_MS - elapsed)
            }
            _state.value = when (result) {
                is SyncResult.Ok ->
                    _state.value.copy(
                        syncing = false,
                        // Every refusal, not only the first. Each one left a
                        // local row that the app has just undone, so a user who
                        // hears about one of three loses the other two edits
                        // without ever learning why.
                        //
                        // A clean sync keeps whatever message is already on
                        // screen. add() sets one and then calls sync(), so
                        // clearing it here erased the confirmation of the very
                        // task the user had just typed.
                        message = if (result.rejected.isEmpty()) {
                            if (announce) "Synced. The account is at version ${result.version}." else _state.value.message
                        } else {
                            result.rejected.joinToString("\n") { "The server refused a change: $it" }
                        },
                        configured = true,
                    )
                is SyncResult.Failed ->
                    _state.value.copy(syncing = false, message = result.message)
            }
        }
    }

    /** parse runs on every keystroke. The Go parser is fast enough for that. */
    fun parse(text: String): ParsedLine =
        if (text.isBlank()) ParsedLine() else Binding.parseQuickAdd(text, today.value)

    fun add(text: String, onDone: (String) -> Unit) {
        viewModelScope.launch {
            val parsed = Binding.parseQuickAdd(text, today.value)
            val result = repo.add(parsed)
            if (!result.ok) {
                _state.value = _state.value.copy(message = result.notice)
                return@launch
            }
            val where = if (result.projectName.isEmpty()) "" else " to #${result.projectName}"
            val line = listOfNotNull(
                "Added \"${result.title}\"$where.",
                result.notice.ifEmpty { null },
            ).joinToString(" ")
            _state.value = _state.value.copy(message = line)
            onDone(line)
            sync()
        }
    }

    fun toggle(task: TaskEntity) {
        viewModelScope.launch {
            if (task.state == "open") repo.complete(task.id) else repo.uncomplete(task.id)
            sync()
        }
    }

    // --- settings -----------------------------------------------------------

    val serverUrl: String get() = repo.settings.serverUrl
    val token: String get() = repo.settings.token

    fun saveSettings(url: String, token: String) {
        repo.settings.serverUrl = url
        repo.settings.token = token
        _state.value = _state.value.copy(configured = repo.settings.isConfigured)
    }

    fun testConnection(onResult: (String) -> Unit) {
        viewModelScope.launch {
            val line = try {
                repo.testConnection()
            } catch (e: Exception) {
                "The test failed. ${e.message}"
            }
            onResult(line)
        }
    }

    fun resetCache() {
        viewModelScope.launch {
            repo.reset()
            sync()
        }
    }

    private companion object {
        // Long enough that a person sees the spinner, short enough that it
        // never feels like waiting. Only an explicit pull pays it.
        const val MIN_SPINNER_MS = 450L
    }
}
