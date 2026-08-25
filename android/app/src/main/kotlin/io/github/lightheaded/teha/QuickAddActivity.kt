// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha

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
class QuickAddActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            TehaTheme {
                QuickAddWindow(
                    onDone = { line ->
                        Toast.makeText(applicationContext, line, Toast.LENGTH_LONG).show()
                        finish()
                    },
                    onCancel = { finish() },
                )
            }
        }
    }
}

@Composable
private fun QuickAddWindow(onDone: (String) -> Unit, onCancel: () -> Unit) {
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
                )
            }
        }
    }
}
