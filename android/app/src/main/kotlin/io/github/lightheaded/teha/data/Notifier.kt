// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.data

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import io.github.lightheaded.teha.MainActivity
import io.github.lightheaded.teha.R

/**
 * Notifier turns a Nudge into a notification.
 *
 * The repository reports the fact and this file writes the words, exactly as
 * internal/push does on the server. See DECISIONS.md D-020. The two senders
 * therefore say the same thing, and neither one had to be taught the other's
 * wording.
 *
 * One channel. A person who does not want to hear about a comment turns the
 * channel off, and Android holds that setting, so the app keeps no preference
 * of its own.
 */
object Notifier {

    private const val CHANNEL = "teha_household"

    /**
     * show posts one notification per nudge.
     *
     * The tag is the kind and the task, so a second comment on the same task
     * replaces the first rather than stacking. A conversation that runs while
     * the phone is in a pocket must not leave twelve notifications.
     */
    fun show(context: Context, nudges: List<Nudge>) {
        if (nudges.isEmpty() || !allowed(context)) return
        channel(context)
        val manager = NotificationManagerCompat.from(context)
        nudges.forEach { n ->
            val (title, text) = words(n)
            val open = Intent(context, MainActivity::class.java).apply {
                flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
            }
            val tap = android.app.PendingIntent.getActivity(
                context,
                n.taskId.hashCode(),
                open,
                android.app.PendingIntent.FLAG_UPDATE_CURRENT or
                    android.app.PendingIntent.FLAG_IMMUTABLE,
            )
            val note = NotificationCompat.Builder(context, CHANNEL)
                .setSmallIcon(R.drawable.ic_tile_add)
                .setContentTitle(title)
                .setContentText(text)
                .setStyle(NotificationCompat.BigTextStyle().bigText(text))
                .setPriority(NotificationCompat.PRIORITY_DEFAULT)
                .setAutoCancel(true)
                .setContentIntent(tap)
                .build()
            manager.notify("${n.kind}-${n.taskId}", 1, note)
        }
    }

    /**
     * words writes the sentence. It is the only place in the app that does.
     *
     * A name the phone does not know reads as "Someone". That happens before
     * the first household read, and it is better than an account id.
     */
    fun words(n: Nudge): Pair<String, String> {
        val who = n.who.ifEmpty { "Someone" }
        return when (n.kind) {
            Nudge.ASSIGNED -> n.title to "$who gave this to you"
            Nudge.COMMENTED -> n.title to "$who: ${shorten(n.body)}"
            else -> n.title to who
        }
    }

    /** shorten keeps a comment to the width a notification can draw. */
    private fun shorten(body: String): String {
        val one = body.replace('\n', ' ').trim()
        return if (one.length <= 120) one else one.take(119).trimEnd() + "…"
    }

    /**
     * allowed reports whether this build may post a notification.
     *
     * Android 13 asks for the permission. Without it every notify() is dropped
     * silently, so the check is here rather than a surprise in the log.
     */
    fun allowed(context: Context): Boolean {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return true
        return ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) ==
            PackageManager.PERMISSION_GRANTED
    }

    private fun channel(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        if (manager.getNotificationChannel(CHANNEL) != null) return
        manager.createNotificationChannel(
            NotificationChannel(
                CHANNEL,
                "The household",
                NotificationManager.IMPORTANCE_DEFAULT,
            ).apply {
                description = "A task somebody gave you, and what they said about it."
            }
        )
    }
}
