// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import io.github.lightheaded.teha.TehaApp
import io.github.lightheaded.teha.data.SyncResult
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
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

enum class TaskView { TODAY, ALL }

data class UiState(
    val view: TaskView = TaskView.TODAY,
    val syncing: Boolean = false,
    val message: String? = null,
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
    @OptIn(ExperimentalCoroutinesApi::class)
    val tasks: StateFlow<List<TaskEntity>> =
        combine(_state.map { it.view }.distinctUntilChanged(), today) { view, day -> view to day }
            .flatMapLatest { (view, day) ->
                when (view) {
                    TaskView.TODAY -> repo.todayTasks(day)
                    TaskView.ALL -> repo.openTasks()
                }
            }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    val projects: StateFlow<List<ProjectEntity>> = repo.projects
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    init {
        viewModelScope.launch {
            repo.outboxCount.collect { n -> _state.value = _state.value.copy(queued = n) }
        }
        if (repo.settings.isConfigured) sync()
    }

    fun setView(v: TaskView) {
        _state.value = _state.value.copy(view = v)
    }

    fun dismissMessage() {
        _state.value = _state.value.copy(message = null)
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
