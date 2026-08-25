// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.github.lightheaded.teha.parser.Binding
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The shared corpus, run through the gomobile binding.
 *
 * parser-fixtures/quickadd.json is the contract between every client. The Go
 * test reads the same file. A drift between the two therefore fails here rather
 * than on a phone.
 *
 * The test is instrumented, not a JVM test, because the .aar carries an Android
 * .so. A JVM run has no native library to load.
 */
@RunWith(AndroidJUnit4::class)
class ParserCorpusTest {

    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun everyCaseMatchesGo() {
        val context = InstrumentationRegistry.getInstrumentation().context
        val text = context.assets.open("quickadd.json").bufferedReader().use { it.readText() }
        val root = json.parseToJsonElement(text).jsonObject
        val today = root["today"]!!.jsonPrimitive.content
        val cases = root["cases"]!!.jsonArray

        assertTrue("the corpus is empty", cases.size > 0)

        val failures = mutableListOf<String>()
        for (element in cases) {
            val case = element.jsonObject
            val input = case["in"]!!.jsonPrimitive.content
            val got = Binding.parseQuickAdd(input, today)

            diff(case, "title", got.title, "")?.let { failures.add("$input: $it") }
            diff(case, "due", got.due, "")?.let { failures.add("$input: $it") }
            diff(case, "time", got.time, "")?.let { failures.add("$input: $it") }
            diff(case, "project", got.project, "")?.let { failures.add("$input: $it") }
            diff(case, "rrule", got.rrule, "")?.let { failures.add("$input: $it") }

            val wantPriority = case["priority"]?.jsonPrimitive?.intOrNull ?: 0
            if (got.priority != wantPriority) {
                failures.add("$input: priority is ${got.priority}, want $wantPriority")
            }

            val wantLabels = case["labels"]?.jsonArray?.map { it.jsonPrimitive.content } ?: emptyList()
            if (got.labels != wantLabels) {
                failures.add("$input: labels are ${got.labels}, want $wantLabels")
            }
        }
        assertEquals("the corpus does not match", emptyList<String>(), failures)
    }

    /** diff returns a message when a field differs, and null when it agrees. */
    private fun diff(case: JsonObject, key: String, got: String, empty: String): String? {
        val want = case[key]?.jsonPrimitive?.contentOrNull ?: empty
        return if (got == want) null else "$key is \"$got\", want \"$want\""
    }

    @Test
    fun newIdIsSortableAndUnique() {
        val first = Binding.newId("t")
        val second = Binding.newId("t")
        assertTrue("the prefix is missing: $first", first.startsWith("t_"))
        assertTrue("two identifiers are equal", first != second)
    }

    @Test
    fun filterCompiles() {
        val compiled = Binding.compileFilter("today | overdue", "2026-08-25")
        assertEquals("", compiled.error)
        assertTrue("no SQL came back", compiled.sql.isNotEmpty())
    }
}
