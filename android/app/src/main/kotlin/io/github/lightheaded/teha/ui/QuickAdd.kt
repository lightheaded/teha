// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Send
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import io.github.lightheaded.teha.parser.ParsedLine
import io.github.lightheaded.teha.ui.theme.priorityColor

/**
 * QuickAddBar is the capture field.
 *
 * The chips come from the Go parser on every keystroke, so the user sees what
 * the line means before it is written. That feedback is the reason the parser is
 * shared and not written again in Kotlin: the chips and the stored task cannot
 * disagree.
 */
@Composable
fun QuickAddBar(
    parse: (String) -> ParsedLine,
    onSubmit: (String) -> Unit,
    modifier: Modifier = Modifier,
    autoFocus: Boolean = false,
    // start seeds the field. The share target hands over a line another app
    // wrote, and it is a draft: the user can edit it, add a date or a project,
    // and only then send it.
    start: String = "",
) {
    var text by remember { mutableStateOf(start) }
    val parsed = remember(text) { parse(text) }
    val focus = remember { FocusRequester() }

    LaunchedEffect(autoFocus) {
        if (autoFocus) focus.requestFocus()
    }

    val submit = {
        if (text.isNotBlank()) {
            onSubmit(text)
            text = ""
        }
    }

    Column(modifier = modifier.fillMaxWidth()) {
        ParsedChips(parsed)
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            OutlinedTextField(
                value = text,
                onValueChange = { text = it },
                modifier = Modifier
                    .weight(1f)
                    .focusRequester(focus),
                singleLine = true,
                label = { Text("Add a task") },
                placeholder = { Text("Buy milk tomorrow p1 #Home @errand") },
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(onDone = { submit() }),
            )
            IconButton(onClick = submit, enabled = text.isNotBlank()) {
                Icon(Icons.Filled.Send, contentDescription = "Add the task")
            }
        }
    }
}

/** ParsedChips names every field that the parser took out of the line. */
@Composable
fun ParsedChips(parsed: ParsedLine, modifier: Modifier = Modifier) {
    val chips: List<Pair<String, Color?>> = buildList {
        if (parsed.due.isNotEmpty()) add("due ${parsed.due}" to null)
        if (parsed.time.isNotEmpty()) add(parsed.time to null)
        if (parsed.priority != 0) add("p${parsed.priority}" to priorityColor(parsed.priority))
        if (parsed.project.isNotEmpty()) add("#${parsed.project}" to null)
        parsed.labels.forEach { add("@$it" to null) }
        if (parsed.rrule.isNotEmpty()) add("repeats" to null)
    }
    if (chips.isEmpty()) return
    LazyRow(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        items(chips.size) { i ->
            val label = chips[i].first
            val color = chips[i].second
            AssistChip(
                onClick = {},
                label = { Text(label) },
                colors = if (color == null) {
                    AssistChipDefaults.assistChipColors()
                } else {
                    AssistChipDefaults.assistChipColors(labelColor = color)
                },
            )
        }
    }
}
