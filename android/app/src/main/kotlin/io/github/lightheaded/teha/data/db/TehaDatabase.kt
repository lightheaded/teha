// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.data.db

import android.content.Context
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase
import androidx.room.TypeConverters
import androidx.room.migration.Migration
import androidx.sqlite.db.SupportSQLiteDatabase

@Database(
    entities = [
        TaskEntity::class,
        ProjectEntity::class,
        SectionEntity::class,
        AccountEntity::class,
        CommentEntity::class,
        LabelEntity::class,
        OutboxEntity::class,
        MetaEntity::class,
    ],
    version = 3,
    // The schema of every version is written into app/schemas and committed.
    // MigrationTestHelper reads it, so a migration written from version 3 on
    // has a test that runs on a device. See MigrationTest.kt.
    exportSchema = true,
)
@TypeConverters(Converters::class)
abstract class TehaDatabase : RoomDatabase() {
    abstract fun tasks(): TaskDao
    abstract fun projects(): ProjectDao
    abstract fun sections(): SectionDao
    abstract fun accounts(): AccountDao
    abstract fun comments(): CommentDao
    abstract fun labels(): LabelDao
    abstract fun outbox(): OutboxDao
    abstract fun meta(): MetaDao

    companion object {
        /**
         * Version 2 is the household: a task carries a section and an
         * assignee, and the phone keeps the sections and the people.
         *
         * A real migration and not the destructive fallback, because the
         * outbox is the one table the server cannot rebuild. Dropping it would
         * throw away a capture that has not reached the server, which is the
         * one promise the app makes about writing offline.
         *
         * The task, project and label tables are a cache, so this migration
         * only adds. The next sync fills the new columns, and a task in an
         * upgraded database is in no section and assigned to nobody until it
         * does, which is what it was before the columns existed.
         *
         * The statements must match what Room generates for the entities in
         * Entities.kt, column for column: Room compares the two at open time
         * and refuses a database that disagrees.
         */
        val MIGRATION_1_2 = object : Migration(1, 2) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL("ALTER TABLE `tasks` ADD COLUMN `sectionId` TEXT")
                db.execSQL("ALTER TABLE `tasks` ADD COLUMN `assigneeId` TEXT")
                db.execSQL(
                    "CREATE TABLE IF NOT EXISTS `sections` (" +
                        "`id` TEXT NOT NULL, `projectId` TEXT NOT NULL, `name` TEXT NOT NULL, " +
                        "`orderKey` TEXT NOT NULL, `deletedAt` TEXT, `version` INTEGER NOT NULL, " +
                        "PRIMARY KEY(`id`))"
                )
                db.execSQL(
                    "CREATE TABLE IF NOT EXISTS `accounts` (" +
                        "`id` TEXT NOT NULL, `name` TEXT NOT NULL, `isMe` INTEGER NOT NULL, " +
                        "`isOwner` INTEGER NOT NULL, PRIMARY KEY(`id`))"
                )
                // The cache is now behind the server in a way a delta cannot
                // describe: every task needs its section and its assignee, and
                // both are null. Drop the watermark so the next sync pulls
                // everything again. The outbox is untouched.
                db.execSQL("DELETE FROM `meta`")
            }
        }

        /**
         * Version 3 is the talk on a task.
         *
         * A real migration, for the same reason as version 2: the outbox is
         * the one table the server cannot rebuild.
         *
         * The watermark goes, exactly as it did in version 2. A comment
         * written before this build has a version below the watermark, so a
         * delta above the watermark never carries it. Without the full pull
         * the talk on a task would start empty and fill only with what is
         * said from now on. The outbox is untouched.
         *
         * The statement must match what Room generates for CommentEntity,
         * column for column: Room compares the two at open time and refuses a
         * database that disagrees.
         */
        val MIGRATION_2_3 = object : Migration(2, 3) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL(
                    "CREATE TABLE IF NOT EXISTS `comments` (" +
                        "`id` TEXT NOT NULL, `taskId` TEXT NOT NULL, `accountId` TEXT NOT NULL, " +
                        "`body` TEXT NOT NULL, `createdAt` TEXT NOT NULL, `deletedAt` TEXT, " +
                        "`version` INTEGER NOT NULL, PRIMARY KEY(`id`))"
                )
                db.execSQL("DELETE FROM `meta`")
            }
        }

        @Volatile
        private var instance: TehaDatabase? = null

        fun get(context: Context): TehaDatabase =
            instance ?: synchronized(this) {
                instance ?: Room.databaseBuilder(
                    context.applicationContext,
                    TehaDatabase::class.java,
                    "teha.db",
                )
                    .addMigrations(MIGRATION_1_2, MIGRATION_2_3)
                    // The fallback stays for a path that has no migration at
                    // all, which is a downgrade or a version nobody wrote a
                    // step for. It costs one full pull, and it costs the
                    // outbox, so every real schema change gets a migration
                    // above instead.
                    .fallbackToDestructiveMigration(dropAllTables = true)
                    .build().also { instance = it }
            }
    }
}
