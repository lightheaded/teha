// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha

import android.app.Application
import io.github.lightheaded.teha.data.SyncWorker
import io.github.lightheaded.teha.data.TehaRepository

/**
 * TehaApp holds the one repository.
 *
 * The app has no dependency injection framework. One object graph with one node
 * needs none, and the quick add activity and the tile reach the same instance
 * through the application.
 */
class TehaApp : Application() {
    val repository: TehaRepository by lazy { TehaRepository(this) }

    override fun onCreate() {
        super.onCreate()
        // The background sync. It drains the outbox and pulls when nobody has
        // the app open, which is what makes an offline capture reach the
        // server and a task somebody gave you arrive. See SyncWorker.
        SyncWorker.schedule(this)
    }
}
