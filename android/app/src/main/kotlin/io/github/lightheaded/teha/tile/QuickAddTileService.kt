// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.tile

import android.app.PendingIntent
import android.content.Intent
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import io.github.lightheaded.teha.QuickAddActivity

/**
 * QuickAddTileService puts capture in the Quick Settings panel.
 *
 * A task arrives while the phone is in a person's hand and while the thought is
 * still there. Two taps from any screen is the whole point of the Android
 * client. A launcher icon is three taps and a context switch.
 */
class QuickAddTileService : TileService() {

    override fun onStartListening() {
        super.onStartListening()
        qsTile?.let {
            it.state = Tile.STATE_INACTIVE
            it.label = "teha add"
            it.updateTile()
        }
    }

    override fun onClick() {
        super.onClick()
        val intent = Intent(this, QuickAddActivity::class.java).apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        }
        // Android 14 refuses startActivityAndCollapse with an Intent and needs a
        // PendingIntent. The older call is the only one that works below that.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            val pending = PendingIntent.getActivity(
                this,
                0,
                intent,
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
            )
            startActivityAndCollapse(pending)
        } else {
            @Suppress("DEPRECATION")
            startActivityAndCollapse(intent)
        }
    }
}
