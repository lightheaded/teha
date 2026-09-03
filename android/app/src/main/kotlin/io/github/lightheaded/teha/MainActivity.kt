// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha

import android.Manifest
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.viewmodel.compose.viewModel
import io.github.lightheaded.teha.data.Notifier
import io.github.lightheaded.teha.ui.SettingsScreen
import io.github.lightheaded.teha.ui.TaskListScreen
import io.github.lightheaded.teha.ui.TehaViewModel
import io.github.lightheaded.teha.ui.theme.TehaTheme

/**
 * AskForNotifications asks once, on Android 13 and later.
 *
 * The app works with the permission refused: the list still fills, and the
 * only thing lost is being told before you look. So the ask happens on the
 * first open and never again, and nothing here explains itself twice.
 */
@Composable
private fun AskForNotifications() {
    val context = LocalContext.current
    val ask = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { /* Either answer is fine. Notifier checks the permission before it posts. */ }
    LaunchedEffect(Unit) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            !Notifier.allowed(context)
        ) {
            ask.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }
}

class MainActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            TehaTheme {
                val vm: TehaViewModel = viewModel()
                AskForNotifications()
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
