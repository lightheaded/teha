// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.data.db

import android.content.Context
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase
import androidx.room.TypeConverters

@Database(
    entities = [
        TaskEntity::class,
        ProjectEntity::class,
        LabelEntity::class,
        OutboxEntity::class,
        MetaEntity::class,
    ],
    version = 1,
    exportSchema = false,
)
@TypeConverters(Converters::class)
abstract class TehaDatabase : RoomDatabase() {
    abstract fun tasks(): TaskDao
    abstract fun projects(): ProjectDao
    abstract fun labels(): LabelDao
    abstract fun outbox(): OutboxDao
    abstract fun meta(): MetaDao

    companion object {
        @Volatile
        private var instance: TehaDatabase? = null

        fun get(context: Context): TehaDatabase =
            instance ?: synchronized(this) {
                instance ?: Room.databaseBuilder(
                    context.applicationContext,
                    TehaDatabase::class.java,
                    "teha.db",
                )
                    // The task, project and label tables are a cache of the
                    // command log, so a schema change costs one full pull.
                    // The outbox is not a cache. A future schema change must
                    // therefore drain the outbox first, or add a migration.
                    .fallbackToDestructiveMigration(dropAllTables = true)
                    .build().also { instance = it }
            }
    }
}
