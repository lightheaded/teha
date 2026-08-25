// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.data

import android.content.Context
import android.content.SharedPreferences
import android.util.Log
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey

/**
 * Settings holds the server address and the device token.
 *
 * The token is a bearer credential for the whole account, so it goes into
 * EncryptedSharedPreferences and never into a log line or a crash report.
 */
class Settings(context: Context) {

    private val prefs: SharedPreferences = open(context)

    var serverUrl: String
        get() = prefs.getString(KEY_SERVER, "") ?: ""
        set(value) = prefs.edit().putString(KEY_SERVER, normalize(value)).apply()

    var token: String
        get() = prefs.getString(KEY_TOKEN, "") ?: ""
        set(value) = prefs.edit().putString(KEY_TOKEN, value.trim()).apply()

    val isConfigured: Boolean
        get() = serverUrl.isNotEmpty()

    companion object {
        private const val KEY_SERVER = "server_url"
        private const val KEY_TOKEN = "device_token"
        private const val FILE = "teha_secure"

        /** normalize removes a trailing slash, so that path joins stay simple. */
        fun normalize(url: String): String = url.trim().trimEnd('/')

        private fun open(context: Context): SharedPreferences {
            return try {
                val key = MasterKey.Builder(context)
                    .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
                    .build()
                EncryptedSharedPreferences.create(
                    context,
                    FILE,
                    key,
                    EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
                    EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
                )
            } catch (_: Exception) {
                // A damaged hardware keystore makes the encrypted file
                // unreadable for good. A crash here locks the user out of the
                // app, so the file is dropped and the user types the token
                // again.
                Log.w("Settings", "the encrypted store failed, it will be rebuilt")
                context.deleteSharedPreferences(FILE)
                val key = MasterKey.Builder(context)
                    .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
                    .build()
                EncryptedSharedPreferences.create(
                    context,
                    FILE,
                    key,
                    EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
                    EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
                )
            }
        }
    }
}
