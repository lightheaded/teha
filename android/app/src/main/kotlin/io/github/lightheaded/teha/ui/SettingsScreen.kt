// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(vm: TehaViewModel, onBack: () -> Unit) {
    var url by remember { mutableStateOf(vm.serverUrl) }
    var token by remember { mutableStateOf(vm.token) }
    var result by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Settings") },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
                    }
                },
            )
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .padding(padding)
                .padding(16.dp)
                .verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            OutlinedTextField(
                value = url,
                onValueChange = { url = it },
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
                label = { Text("Server address") },
                placeholder = { Text("https://tasks.example.org") },
                supportingText = { Text("The address of your own server. No trailing slash.") },
            )
            OutlinedTextField(
                value = token,
                onValueChange = { token = it },
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
                label = { Text("Device token") },
                visualTransformation = PasswordVisualTransformation(),
                supportingText = { Text("The token goes into the encrypted store. It never reaches a log.") },
            )
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Button(
                    onClick = {
                        vm.saveSettings(url, token)
                        result = "Saved."
                    }
                ) { Text("Save") }
                OutlinedButton(
                    enabled = !busy,
                    onClick = {
                        vm.saveSettings(url, token)
                        busy = true
                        result = "Testing..."
                        vm.testConnection { line ->
                            result = line
                            busy = false
                        }
                    },
                ) { Text("Test connection") }
            }
            if (result.isNotEmpty()) {
                Text(result, style = MaterialTheme.typography.bodyMedium)
            }
            OutlinedButton(
                onClick = {
                    vm.resetCache()
                    result = "The local cache is empty. The next sync pulls everything."
                },
                modifier = Modifier.fillMaxWidth(),
            ) { Text("Clear the local cache") }
            Text(
                "The test asks GET /v1/health first, and then POST /v1/sync with the " +
                    "token. A wrong token fails the second call only.",
                style = MaterialTheme.typography.bodySmall,
            )
        }
    }
}
