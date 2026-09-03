// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha

import android.content.Intent
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import io.github.lightheaded.teha.data.SyncWorker
import io.github.lightheaded.teha.ui.QuickAddBar
import io.github.lightheaded.teha.ui.TehaViewModel
import io.github.lightheaded.teha.ui.theme.TehaTheme

/**
 * QuickAddActivity is the capture window that the Quick Settings tile opens.
 *
 * It is transparent and it holds one field. The window closes as soon as the
 * task reaches the outbox, so a capture costs one tap and one line. The task is
 * written locally before any network call, so a dead network changes nothing.
 */
open class QuickAddActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val start = startingText(intent)
        setContent {
            TehaTheme {
                QuickAddWindow(
                    start = start,
                    onDone = { line ->
                        Toast.makeText(applicationContext, line, Toast.LENGTH_LONG).show()
                        // The window closes at once, so nothing is left open
                        // to carry the task to the server. The worker does it.
                        SyncWorker.soon(applicationContext)
                        finish()
                    },
                    onCancel = { finish() },
                )
            }
        }
    }

    /**
     * startingText reads what another app shared.
     *
     * The tile carries nothing, so the field opens empty. A share carries the
     * text, and a shared web page carries a title as well: the two together
     * read as "Title https://…", which is a task a person can recognise a week
     * later. A bare URL is not.
     *
     * The line is a draft and never a task. The user still presses send, so a
     * share that lands in the wrong app costs nothing.
     */
    private fun startingText(intent: Intent?): String {
        if (intent?.action != Intent.ACTION_SEND) return ""
        val text = intent.getStringExtra(Intent.EXTRA_TEXT).orEmpty().trim()
        val subject = intent.getStringExtra(Intent.EXTRA_SUBJECT).orEmpty().trim()
        return when {
            subject.isEmpty() -> text
            text.isEmpty() -> subject
            text.startsWith(subject) -> text
            else -> "$subject $text"
        }.take(MAX_SHARED)
    }

    private companion object {
        // A shared article can carry a whole paragraph. A task title is one
        // line, and the rest is noise in every view that draws it.
        const val MAX_SHARED = 200
    }
}

/**
 * ShareActivity is the same capture window, opened from the system share
 * sheet.
 *
 * It is a separate class because it is exported and QuickAddActivity is not.
 * The tile starts the capture window with this app's own identity, so nothing
 * else needs to. A share target must be exported, and this one accepts one
 * action and one type: it reads the text, it fills the field, and the user
 * still presses send.
 */
class ShareActivity : QuickAddActivity()

@Composable
private fun QuickAddWindow(start: String, onDone: (String) -> Unit, onCancel: () -> Unit) {
    val vm: TehaViewModel = viewModel()
    val scrim = remember { MutableInteractionSource() }
    val card = remember { MutableInteractionSource() }

    Box(
        modifier = Modifier
            .fillMaxSize()
            // A tap outside the card closes the window. No ripple, because the
            // scrim is not a control.
            .clickable(interactionSource = scrim, indication = null, onClick = onCancel)
            .imePadding()
            .padding(16.dp),
        contentAlignment = Alignment.BottomCenter,
    ) {
        Surface(
            modifier = Modifier
                .fillMaxWidth()
                // The card swallows a tap, so the scrim below does not close it.
                .clickable(interactionSource = card, indication = null, onClick = {}),
            shape = MaterialTheme.shapes.large,
            tonalElevation = 6.dp,
            shadowElevation = 8.dp,
        ) {
            Column(modifier = Modifier.padding(vertical = 8.dp)) {
                Text(
                    "teha",
                    style = MaterialTheme.typography.labelLarge,
                    modifier = Modifier.padding(start = 24.dp, top = 8.dp),
                )
                QuickAddBar(
                    parse = vm::parse,
                    onSubmit = { text -> vm.add(text) { line -> onDone(line) } },
                    autoFocus = true,
                    start = start,
                )
            }
        }
    }
}
