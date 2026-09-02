// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha

import androidx.room.Room
import androidx.sqlite.db.SimpleSQLiteQuery
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.github.lightheaded.teha.data.db.TaskEntity
import io.github.lightheaded.teha.data.db.TehaDatabase
import io.github.lightheaded.teha.parser.Binding
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The compiled filter, run against a real Room database.
 *
 * The Go test in filter/schema_test.go proves the meaning of every term against
 * SQLite. This test proves the two joints that only a device shows: the
 * @RawQuery method with a Flow, and the column names of the Room database that
 * Room itself created.
 */
@RunWith(AndroidJUnit4::class)
class RoomFilterTest {

    private lateinit var db: TehaDatabase

    private val today = "2026-08-25"

    @Before
    fun open() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        db = Room.inMemoryDatabaseBuilder(context, TehaDatabase::class.java).build()
    }

    @After
    fun close() {
        db.close()
    }

    /** rows reads one filter, the way TehaRepository.tasks does. */
    private suspend fun rows(query: String): List<String> {
        val compiled = Binding.compileFilterRoom(query, today)
        assertEquals("the compiler refused $query", "", compiled.error)
        val sql = "SELECT * FROM tasks WHERE deletedAt IS NULL AND (${compiled.sql}) " +
            "ORDER BY (dueDate IS NULL), dueDate ASC, priority ASC, orderKey ASC"
        return db.tasks()
            .filtered(SimpleSQLiteQuery(sql, compiled.argValues.toTypedArray()))
            .first()
            .map { it.id }
    }

    @Test
    fun everyViewReadsTheRoomTables() = runBlocking {
        db.tasks().upsert(
            listOf(
                task("t1", title = "Fix the sink", due = "2026-08-24", priority = 3, labels = listOf("work")),
                task("t2", title = "Do homework", due = today, priority = 1, labels = listOf("homework")),
                task("t3", title = "Buy milk"),
                task("t4", title = "Wash the car", due = "2026-08-24", state = "done"),
            )
        )

        // The overdue task sorts above today, which is the order of the view.
        assertEquals(listOf("t1", "t2"), rows("today"))
        assertEquals(listOf("t1"), rows("overdue"))
        assertEquals(listOf("t3"), rows("no date"))
        assertEquals(listOf("t2"), rows("p1"))
        assertEquals(listOf("t1", "t2", "t3"), rows(""))
        assertEquals(listOf("t4"), rows("done"))
        // A label lives in a comma-joined column here, so the match is a LIKE
        // over a padded copy of it. work must not find homework.
        assertEquals(listOf("t1"), rows("%work"))
        assertEquals(listOf("t2"), rows("%homework"))
        assertEquals(listOf("t3"), rows("no labels"))
        assertEquals(listOf("t3"), rows("search: milk"))
        assertEquals(listOf("t1", "t2"), rows("overdue | p1"))
    }

    /** A filter the compiler refuses carries a message and no SQL. */
    @Test
    fun aBadFilterSaysWhatIsWrong() {
        val compiled = Binding.compileFilterRoom("today &", today)
        assertEquals("", compiled.sql)
        assertTrue("no message came back", compiled.error.isNotEmpty())
    }

    private fun task(
        id: String,
        title: String,
        due: String? = null,
        priority: Int = 4,
        state: String = "open",
        labels: List<String> = emptyList(),
    ) = TaskEntity(
        id = id,
        projectId = "inbox",
        sectionId = null,
        assigneeId = null,
        parentId = null,
        orderKey = "m",
        title = title,
        description = "",
        priority = priority,
        dueDate = due,
        dueTime = null,
        dueTz = null,
        rrule = null,
        rruleFromCompletion = false,
        startDate = null,
        deadline = null,
        durationMin = null,
        state = state,
        completedAt = null,
        deletedAt = null,
        labels = labels,
        version = 0,
    )
}
