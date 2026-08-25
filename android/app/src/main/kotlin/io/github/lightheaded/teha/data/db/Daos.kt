// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.data.db

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

@Dao
interface TaskDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(tasks: List<TaskEntity>)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertOne(task: TaskEntity)

    @Query("SELECT * FROM tasks WHERE id = :id")
    suspend fun byId(id: String): TaskEntity?

    // The Today view. An overdue day sorts before today, so the list needs no
    // second query and no grouping pass.
    @Query(
        """
        SELECT * FROM tasks
        WHERE deletedAt IS NULL AND state = 'open'
          AND dueDate IS NOT NULL AND dueDate <= :today
        ORDER BY dueDate ASC, priority ASC, orderKey ASC
        """
    )
    fun today(today: String): Flow<List<TaskEntity>>

    @Query(
        """
        SELECT * FROM tasks
        WHERE deletedAt IS NULL AND state = 'open'
        ORDER BY (dueDate IS NULL), dueDate ASC, priority ASC, orderKey ASC
        """
    )
    fun allOpen(): Flow<List<TaskEntity>>

    @Query("DELETE FROM tasks")
    suspend fun clear()
}

@Dao
interface ProjectDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(projects: List<ProjectEntity>)

    @Query("SELECT * FROM projects WHERE deletedAt IS NULL ORDER BY orderKey ASC, name ASC")
    fun all(): Flow<List<ProjectEntity>>

    @Query("SELECT * FROM projects WHERE deletedAt IS NULL")
    suspend fun allNow(): List<ProjectEntity>

    @Query("DELETE FROM projects")
    suspend fun clear()
}

@Dao
interface LabelDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(labels: List<LabelEntity>)

    @Query("SELECT * FROM labels WHERE deletedAt IS NULL ORDER BY name ASC")
    fun all(): Flow<List<LabelEntity>>

    @Query("DELETE FROM labels")
    suspend fun clear()
}

@Dao
interface OutboxDao {
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun add(row: OutboxEntity)

    @Query("SELECT * FROM outbox ORDER BY createdAt ASC LIMIT :limit")
    suspend fun oldest(limit: Int): List<OutboxEntity>

    @Query("DELETE FROM outbox WHERE uuid IN (:uuids)")
    suspend fun remove(uuids: List<String>)

    @Query("SELECT count(*) FROM outbox")
    fun countFlow(): Flow<Int>

    @Query("DELETE FROM outbox")
    suspend fun clear()
}

@Dao
interface MetaDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun put(row: MetaEntity)

    @Query("SELECT value FROM meta WHERE key = :key")
    suspend fun get(key: String): String?

    @Query("DELETE FROM meta")
    suspend fun clear()
}
