// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext

private val Green = Color(0xFF3F8F6E)
private val GreenLight = Color(0xFF7FD1AE)

private val DarkScheme = darkColorScheme(primary = GreenLight, secondary = Green)
private val LightScheme = lightColorScheme(primary = Green, secondary = GreenLight)

/** Priority 1 is the highest. That is the opposite of the Todoist API. */
val priorityColors = listOf(
    Color(0xFFD32F2F), // p1
    Color(0xFFF57C00), // p2
    Color(0xFF1976D2), // p3
    Color(0xFF9E9E9E), // p4
)

fun priorityColor(priority: Int): Color =
    priorityColors.getOrElse(priority - 1) { priorityColors[3] }

@Composable
fun TehaTheme(dark: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    val context = LocalContext.current
    val scheme = when {
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.S ->
            if (dark) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
        dark -> DarkScheme
        else -> LightScheme
    }
    MaterialTheme(colorScheme = scheme, content = content)
}
