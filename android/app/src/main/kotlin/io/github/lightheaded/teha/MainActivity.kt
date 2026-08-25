// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.viewmodel.compose.viewModel
import io.github.lightheaded.teha.ui.SettingsScreen
import io.github.lightheaded.teha.ui.TaskListScreen
import io.github.lightheaded.teha.ui.TehaViewModel
import io.github.lightheaded.teha.ui.theme.TehaTheme

class MainActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            TehaTheme {
                val vm: TehaViewModel = viewModel()
                // A pull on every open keeps the list close to the server
                // without a background service. GET /v1/events replaces this
                // poll later.
                LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.sync() }

                // Two screens do not need a navigation graph. One flag costs
                // nothing and it keeps the dependency list shorter.
                var settings by remember { mutableStateOf(false) }
                if (settings) {
                    SettingsScreen(vm = vm, onBack = { settings = false })
                } else {
                    TaskListScreen(vm = vm, onOpenSettings = { settings = true })
                }
            }
        }
    }
}
