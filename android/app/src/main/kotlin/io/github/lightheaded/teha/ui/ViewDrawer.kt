// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.NavigationDrawerItemDefaults
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import io.github.lightheaded.teha.data.db.ProjectEntity

/**
 * ViewDrawerSheet is the navigation of the list screen.
 *
 * A drawer, because the browser sidebar holds the same two groups and a phone
 * has no room for seven views and every project in one row of chips. The swipe
 * from the left edge opens it, which is one gesture, and the menu button in the
 * top bar is the other way in.
 *
 * The filter field sits above the views, and it takes any query the grammar
 * knows. The phone runs the real compiler through the gomobile binding, so it
 * reads every term the server reads.
 *
 * onFilter reports whether the query was accepted. A refusal keeps the drawer
 * open and shows the message under the field, because that message names the
 * position that failed and the user needs the field to fix it.
 */
@Composable
fun ViewDrawerSheet(
    current: TaskView,
    projects: List<ProjectEntity>,
    filterError: String?,
    onPick: (TaskView) -> Unit,
    onFilter: (String) -> Boolean,
    onTyping: () -> Unit,
) {
    var text by rememberSaveable { mutableStateOf("") }
    // The inbox is a built-in view already, so it appears once. The browser
    // drops the same row from its project list.
    val named = projects.filter { !it.isInbox }

    ModalDrawerSheet {
        Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
            Text(
                "teha",
                style = MaterialTheme.typography.titleLarge,
                modifier = Modifier.padding(start = 28.dp, top = 20.dp, bottom = 8.dp),
            )
            OutlinedTextField(
                value = text,
                onValueChange = { text = it; onTyping() },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = 4.dp),
                singleLine = true,
                isError = filterError != null,
                label = { Text("Filter") },
                placeholder = { Text("overdue & #Home") },
                supportingText = {
                    Text(filterError ?: "A query, or nothing for every open task.")
                },
                trailingIcon = {
                    IconButton(onClick = { onFilter(text) }) {
                        Icon(Icons.Filled.Search, contentDescription = "Show the filter")
                    }
                },
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                keyboardActions = KeyboardActions(onSearch = { onFilter(text) }),
            )
            BUILT_IN_VIEWS.forEach { view ->
                DrawerRow(view.title, view.query == current.query) { onPick(view) }
            }
            if (named.isNotEmpty()) {
                HorizontalDivider(modifier = Modifier.padding(horizontal = 28.dp, vertical = 8.dp))
                Text(
                    "Projects",
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = 28.dp, bottom = 4.dp),
                )
                named.forEach { project ->
                    val view = projectView(project)
                    DrawerRow(project.name, view.query == current.query) { onPick(view) }
                }
            }
        }
    }
}

@Composable
private fun DrawerRow(title: String, selected: Boolean, onClick: () -> Unit) {
    NavigationDrawerItem(
        label = { Text(title) },
        selected = selected,
        onClick = onClick,
        modifier = Modifier.padding(NavigationDrawerItemDefaults.ItemPadding),
    )
}
