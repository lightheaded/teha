// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.data

import android.content.Context
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import io.github.lightheaded.teha.TehaApp
import java.util.concurrent.TimeUnit

/**
 * SyncWorker drains the outbox and pulls, without the app being open.
 *
 * Until this existed, a capture made on a train reached the server only when
 * somebody opened the app again, and a task the other person gave you was
 * found and never announced. Both are the same missing thing: a sync that runs
 * on its own.
 *
 * A nudge is posted here as well as in the app. Whichever sync sees the row
 * first says it, and the other finds nothing new, so nothing is said twice.
 */
class SyncWorker(context: Context, params: WorkerParameters) :
    CoroutineWorker(context, params) {

    override suspend fun doWork(): Result {
        val app = applicationContext as? TehaApp ?: return Result.success()
        val repo = app.repository
        if (!repo.settings.isConfigured) return Result.success()

        return when (val outcome = repo.sync()) {
            is SyncResult.Ok -> {
                Notifier.show(applicationContext, outcome.nudges)
                Result.success()
            }
            // A failure is almost always a network that is not there. Retry
            // asks WorkManager to try again with its own backoff, which is
            // what the periodic run would do anyway, only sooner.
            is SyncResult.Failed ->
                if (outcome.unauthorized) Result.failure() else Result.retry()
        }
    }

    companion object {
        private const val PERIODIC = "teha-sync-periodic"
        private const val ONCE = "teha-sync-once"

        /**
         * schedule starts the background sync, once per install.
         *
         * Fifteen minutes is the shortest period Android accepts, and it is
         * the floor rather than a promise: the system runs it when it suits
         * the battery. That is the right trade for a household list, and it is
         * why the browser has Web Push and the phone does not yet.
         *
         * KEEP, so an app that starts twice does not reset the schedule and
         * push the next run away.
         */
        fun schedule(context: Context) {
            val work = PeriodicWorkRequestBuilder<SyncWorker>(15, TimeUnit.MINUTES)
                .setConstraints(
                    Constraints.Builder()
                        .setRequiredNetworkType(NetworkType.CONNECTED)
                        .build()
                )
                .build()
            WorkManager.getInstance(context).enqueueUniquePeriodicWork(
                PERIODIC,
                ExistingPeriodicWorkPolicy.KEEP,
                work,
            )
        }

        /**
         * soon asks for one sync as early as the system allows.
         *
         * The capture window uses it: a task typed into the tile is written
         * locally and the window closes, and this carries it to the server
         * even though no screen stays open to do it.
         *
         * REPLACE, because two captures in a row want one sync and not two.
         */
        fun soon(context: Context) {
            val work = OneTimeWorkRequestBuilder<SyncWorker>()
                .setConstraints(
                    Constraints.Builder()
                        .setRequiredNetworkType(NetworkType.CONNECTED)
                        .build()
                )
                .build()
            WorkManager.getInstance(context).enqueueUniqueWork(
                ONCE,
                ExistingWorkPolicy.REPLACE,
                work,
            )
        }
    }
}
