// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material3.Checkbox
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.SuggestionChip
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.github.lightheaded.teha.data.db.SectionEntity
import io.github.lightheaded.teha.data.db.TaskEntity
import io.github.lightheaded.teha.data.itemCount
import io.github.lightheaded.teha.data.itemName
import io.github.lightheaded.teha.data.normalItem
import java.time.Duration
import java.time.Instant

/**
 * ShoppingScreen is a shopping list, read at arm's length in a cold aisle.
 *
 * It is a layout of one project and not a new kind of data. An aisle is a
 * section of that project, and the suggestions are what the household bought
 * before, so nothing here needs a table of its own. See DECISIONS.md D-021 and
 * the same layout in internal/webui/assets/app.js.
 *
 * Three things are different from the task list, and each one is about a hand
 * that is holding a trolley:
 *
 *   - The check target is the whole row, and it is large.
 *   - A count is a chip, because "2" is read faster than "2x milk".
 *   - A ticked item stays on the screen, in the basket, so a wrong tap costs
 *     one tap back.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun ShoppingScreen(
    items: List<TaskEntity>,
    sections: List<SectionEntity>,
    // cleared is the moment somebody emptied the basket of this list, or an
    // empty string. It is a device setting: two people in one shop each empty
    // their own basket.
    cleared: String,
    onToggle: (TaskEntity) -> Unit,
    onOpen: (TaskEntity) -> Unit,
    onAdd: (String) -> Unit,
    onClearBasket: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var fresh by remember { mutableStateOf("") }

    val open = items.filter { it.state == "open" }
    val aisles = groupByAisle(open, sections)
    val bag = basket(items, cleared)
    val suggestions = boughtBefore(items, open)

    Column(modifier = modifier.fillMaxSize()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            OutlinedTextField(
                value = fresh,
                onValueChange = { fresh = it },
                placeholder = { Text("Add an item") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                // The keyboard stays open and the field keeps the caret. A
                // person in a shop types three items in a row, and a field
                // that has to be tapped again each time costs three taps.
                keyboardActions = KeyboardActions(onDone = {
                    if (fresh.isNotBlank()) { onAdd(fresh); fresh = "" }
                }),
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = 56.dp),
            )
            IconButton(
                onClick = { if (fresh.isNotBlank()) { onAdd(fresh); fresh = "" } },
                enabled = fresh.isNotBlank(),
            ) {
                Icon(Icons.Filled.Add, contentDescription = "Add the item")
            }
        }

        // weight, not fillMaxSize. The add field above takes its own height
        // and the list takes the rest, which is what a Column measures.
        LazyColumn(modifier = Modifier.weight(1f)) {
            if (aisles.isEmpty()) {
                item {
                    Text(
                        "Nothing to buy. Type in the box above.",
                        style = MaterialTheme.typography.bodyMedium,
                        modifier = Modifier.padding(24.dp),
                    )
                }
            }
            aisles.forEach { aisle ->
                item(key = "head-${aisle.id ?: "none"}") { AisleHead(aisle.name) }
                items(aisle.items, key = { it.id }) { task ->
                    ShopRow(task, onToggle = { onToggle(task) }, onOpen = { onOpen(task) })
                    HorizontalDivider()
                }
            }

            if (suggestions.isNotEmpty()) {
                item(key = "suggestions") {
                    Column {
                        AisleHead("Bought before")
                        FlowRow(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(horizontal = 12.dp, vertical = 4.dp),
                            horizontalArrangement = Arrangement.spacedBy(6.dp),
                        ) {
                            suggestions.forEach { past ->
                                SuggestionChip(
                                    onClick = { onAdd(past.title) },
                                    label = { Text(itemName(past.title)) },
                                )
                            }
                        }
                    }
                }
            }

            if (bag.isNotEmpty()) {
                item(key = "basket") {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(start = 16.dp, top = 16.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            "In the basket  ${bag.size}",
                            style = MaterialTheme.typography.labelLarge,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        TextButton(onClick = onClearBasket) { Text("Clear") }
                    }
                }
                items(bag, key = { "bag-${it.id}" }) { task ->
                    ShopRow(task, onToggle = { onToggle(task) }, onOpen = { onOpen(task) })
                    HorizontalDivider()
                }
            }
        }
    }
}

@Composable
private fun AisleHead(name: String) {
    Surface(tonalElevation = 2.dp, modifier = Modifier.fillMaxWidth()) {
        Text(
            name,
            style = MaterialTheme.typography.labelLarge,
            fontWeight = FontWeight.Medium,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
        )
    }
}

/**
 * ShopRow is one item.
 *
 * The row is 64 density pixels tall and the whole of it ticks the item. A tap
 * that misses in a cold aisle is a duplicate on the shelf, and a tap on the
 * name opens the item, so a note and a comment are still one tap away.
 */
@Composable
private fun ShopRow(task: TaskEntity, onToggle: () -> Unit, onOpen: () -> Unit) {
    val done = task.state != "open"
    val count = itemCount(task.title)
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 64.dp)
            .padding(end = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Checkbox(
            checked = done,
            onCheckedChange = { onToggle() },
            modifier = Modifier
                .padding(horizontal = 8.dp)
                .size(40.dp),
        )
        if (count.isNotEmpty()) {
            Surface(
                tonalElevation = 4.dp,
                shape = MaterialTheme.shapes.small,
                modifier = Modifier.padding(end = 8.dp),
            ) {
                Text(
                    count,
                    style = MaterialTheme.typography.labelLarge,
                    fontWeight = FontWeight.Bold,
                    modifier = Modifier.padding(horizontal = 8.dp, vertical = 4.dp),
                )
            }
        }
        TextButton(
            onClick = onOpen,
            modifier = Modifier.weight(1f),
        ) {
            Text(
                itemName(task.title),
                fontSize = 17.sp,
                textDecoration = if (done) TextDecoration.LineThrough else null,
                color = if (done) MaterialTheme.colorScheme.onSurfaceVariant
                else MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

/** Aisle is one heading and the items under it. */
data class Aisle(val id: String?, val name: String, val items: List<TaskEntity>)

/**
 * groupByAisle puts every open item under its heading.
 *
 * An empty aisle is not drawn. A list read at arm's length must hold no
 * headings with nothing under them.
 */
fun groupByAisle(open: List<TaskEntity>, sections: List<SectionEntity>): List<Aisle> {
    val order = listOf<String?>(null) + sections.map { it.id }
    val names = mapOf<String?, String>(null to "Anything else") +
        sections.associate { it.id to it.name }
    val byAisle = open.groupBy { task ->
        if (task.sectionId != null && names.containsKey(task.sectionId)) task.sectionId else null
    }
    return order.mapNotNull { id ->
        val rows = byAisle[id].orEmpty()
        if (rows.isEmpty()) null else Aisle(id, names[id] ?: "Anything else", rows)
    }
}

/**
 * TRIP is how long the basket remembers.
 *
 * A trip is hours, not years. Without the window a list used for a year would
 * open with a year of shopping in the basket. The window is what the layout
 * means by "the basket", and the Clear button is what a person means by "done
 * with that".
 */
private val TRIP: Duration = Duration.ofHours(12)

/** basket lists what went in on this trip, newest first. */
fun basket(items: List<TaskEntity>, cleared: String): List<TaskEntity> {
    val window = Instant.now().minus(TRIP).toString()
    val since = if (cleared > window) cleared else window
    return items
        .filter { it.state != "open" && (it.completedAt ?: "") > since }
        .sortedByDescending { it.completedAt ?: "" }
}

/**
 * boughtBefore suggests what the household buys and has not got on the list
 * now. One row per name, newest first.
 */
fun boughtBefore(items: List<TaskEntity>, open: List<TaskEntity>, limit: Int = 10): List<TaskEntity> {
    val already = open.map { normalItem(it.title) }.toSet()
    val seen = mutableSetOf<String>()
    return items
        .filter { it.state != "open" && it.completedAt != null }
        .sortedByDescending { it.completedAt }
        .filter { task ->
            val key = normalItem(task.title)
            key.isNotEmpty() && key !in already && seen.add(key)
        }
        .take(limit)
}
