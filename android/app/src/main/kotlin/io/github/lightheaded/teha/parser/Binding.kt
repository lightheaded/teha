// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.parser

import io.github.lightheaded.mobile.Mobile
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import java.time.LocalDate

/**
 * Binding wraps the gomobile binding.
 *
 * D-002 says that every client runs one parser. The Go code in ./mobile is that
 * parser, and `gomobile bind` turns it into the .aar this file calls. A Kotlin
 * rewrite would be a third implementation that drifts from the corpus.
 *
 * Every binding function returns JSON, and none of them throws. A failure
 * arrives in an "error" key instead. See mobile/mobile.go for the reason.
 */
object Binding {

    private val json = Json { ignoreUnknownKeys = true }

    /** todayIso is read at the call site, so the phone time zone decides the day. */
    fun todayIso(): String = LocalDate.now().toString()

    fun parseQuickAdd(text: String, today: String = todayIso()): ParsedLine =
        json.decodeFromString(ParsedLine.serializer(), Mobile.parseQuickAdd(text, today))

    fun compileFilter(query: String, today: String = todayIso()): CompiledFilter =
        json.decodeFromString(CompiledFilter.serializer(), Mobile.compileFilter(query, today))

    /**
     * compileFilterRoom compiles a filter against the local Room database.
     *
     * The server and Room hold the same rows under different names, so the Go
     * compiler takes the names as a value and emits either dialect. One filter
     * string therefore means one thing here, in the browser and on the server.
     * See filter/schema.go for the mapping.
     *
     * The result is a WHERE clause only. The caller adds the test for a
     * deleted row and the sort of the view.
     *
     * me is the account that is asking, and `assigned to: me` is the one term
     * that needs it. An empty value makes that term fail with a sentence,
     * which is right for a phone that has not joined a household.
     */
    fun compileFilterRoom(
        query: String,
        today: String = todayIso(),
        me: String = "",
    ): CompiledFilter = json.decodeFromString(
        CompiledFilter.serializer(),
        Mobile.compileFilterRoomFor(query, today, me),
    )

    fun nextRecurrence(rule: String, base: String, today: String, fromCompletion: Boolean): Recurrence =
        json.decodeFromString(
            Recurrence.serializer(),
            Mobile.nextRecurrence(rule, base, today, fromCompletion),
        )

    fun validRecurrence(rule: String): Boolean =
        json.decodeFromString(Validity.serializer(), Mobile.validRecurrence(rule)).valid

    /** newId names a row before the server has seen it, so an offline edit works. */
    fun newId(prefix: String): String = Mobile.newID(prefix)
}

@Serializable
data class ParsedLine(
    val title: String = "",
    val due: String = "",
    val time: String = "",
    val priority: Int = 0,
    val project: String = "",
    val labels: List<String> = emptyList(),
    val rrule: String = "",
    val parsed: List<String> = emptyList(),
    val error: String = "",
)

@Serializable
data class CompiledFilter(
    val sql: String = "",
    // filter.Compile emits a number for a priority and a string everywhere
    // else, so the list is not uniform. SQLite binds both as text here.
    val args: List<JsonPrimitive> = emptyList(),
    val error: String = "",
) {
    val argValues: List<String> get() = args.map { it.content }
}

@Serializable
data class Recurrence(
    val due: String = "",
    val error: String = "",
)

@Serializable
data class Validity(
    val valid: Boolean = false,
    val error: String = "",
)
