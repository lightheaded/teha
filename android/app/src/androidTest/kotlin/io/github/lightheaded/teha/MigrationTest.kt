// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha

import android.database.sqlite.SQLiteDatabase
import androidx.room.Room
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.github.lightheaded.teha.data.db.CommentEntity
import io.github.lightheaded.teha.data.db.MetaEntity
import io.github.lightheaded.teha.data.db.OutboxEntity
import io.github.lightheaded.teha.data.db.TehaDatabase
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The migration of a database somebody already has.
 *
 * docs/BACKLOG.md records why this was missing: Room compares the migrated
 * schema with the entities at open time, and the comparison is the test, but
 * running it needs a database at the old version. Nothing exported one.
 *
 * This test makes the old version out of the new one. Room itself creates the
 * file, so every table except the new one is byte-accurate, and then the test
 * takes away exactly what version 3 added and says the file is at version 2.
 * Opening it again runs MIGRATION_2_3 and Room validates the result. A
 * hand-written statement that disagrees with CommentEntity by one character
 * fails here.
 *
 * The outbox is the reason a migration exists at all rather than the
 * destructive fallback, so this test carries a row through it.
 */
@RunWith(AndroidJUnit4::class)
class MigrationTest {

    private val name = "migration-test.db"

    @Before
    fun clean() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        context.deleteDatabase(name)
    }

    @Test
    fun theTalkArrivesAndTheOutboxSurvives() = runBlocking {
        val context = InstrumentationRegistry.getInstrumentation().targetContext

        // 1. A database at the current version, made by Room.
        var db = Room.databaseBuilder(context, TehaDatabase::class.java, name).build()
        db.outbox().add(
            OutboxEntity(
                uuid = "c_owed",
                type = "task_add",
                argsJson = """{"id":"t_owed","title":"Written on a train"}""",
                createdAt = 1L,
            )
        )
        db.meta().put(MetaEntity("sync_version", "412"))
        db.close()

        // 2. Take away what version 3 added, and say the file is version 2.
        //    The comments table is the whole of version 3, so this is exactly
        //    the file an installed app has before the update.
        val path = context.getDatabasePath(name).absolutePath
        val raw = SQLiteDatabase.openDatabase(path, null, SQLiteDatabase.OPEN_READWRITE)
        raw.execSQL("DROP TABLE IF EXISTS `comments`")
        raw.execSQL("PRAGMA user_version = 2")
        raw.close()

        // 3. Open it again. Room runs MIGRATION_2_3 and then compares the
        //    result with the entities. A wrong statement fails right here.
        db = Room.databaseBuilder(context, TehaDatabase::class.java, name)
            .addMigrations(TehaDatabase.MIGRATION_1_2, TehaDatabase.MIGRATION_2_3)
            .build()

        // The one table the server cannot rebuild came through.
        val owed = db.outbox().oldest(10)
        assertEquals("the outbox lost a row", 1, owed.size)
        assertEquals("c_owed", owed[0].uuid)
        assertEquals("task_add", owed[0].type)

        // The watermark went, so the next sync pulls every comment written
        // before this version existed.
        assertNull("the migration must drop the watermark", db.meta().get("sync_version"))

        // The new table works, in the columns the entity declares.
        db.comments().upsertOne(
            CommentEntity(
                id = "cm_1",
                taskId = "t_owed",
                accountId = "owner",
                body = "The green one, not the blue.",
                createdAt = "2026-09-03T09:00:00Z",
                deletedAt = null,
                version = 7,
            )
        )
        val talk = db.comments().forTask("t_owed").first()
        assertEquals(1, talk.size)
        assertEquals("The green one, not the blue.", talk[0].body)
        db.close()
    }
}
