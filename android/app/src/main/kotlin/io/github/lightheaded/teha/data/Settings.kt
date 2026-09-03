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

    /**
     * accountId is who this phone is, and accountName is what to call them.
     *
     * The server answers both on every sync, and `assigned to: me` needs the
     * id. A phone that has never synced holds an empty id, and that term then
     * fails with a sentence rather than answering for the wrong person.
     */
    var accountId: String
        get() = prefs.getString(KEY_ACCOUNT, "") ?: ""
        set(value) = prefs.edit().putString(KEY_ACCOUNT, value.trim()).apply()

    var accountName: String
        get() = prefs.getString(KEY_ACCOUNT_NAME, "") ?: ""
        set(value) = prefs.edit().putString(KEY_ACCOUNT_NAME, value.trim()).apply()

    /**
     * inboxId is the project a capture with no project belongs in.
     *
     * Each account has its own inbox, so the fixed id "inbox" is only right
     * for the owner. The sync answer carries the right one.
     */
    var inboxId: String
        get() = prefs.getString(KEY_INBOX, "") ?: ""
        set(value) = prefs.edit().putString(KEY_INBOX, value.trim()).apply()

    val isConfigured: Boolean
        get() = serverUrl.isNotEmpty()

    /**
     * shopClear is the moment somebody emptied the basket of one list.
     *
     * A completed item stays completed, so the basket needs its own mark for
     * "this trip". The browser keeps the same value per project. It is a
     * device setting and not account data: two people in one shop each empty
     * their own basket, and neither wants the other's screen to change.
     */
    fun shopClear(projectId: String): String =
        prefs.getString(KEY_SHOP_CLEAR + projectId, "") ?: ""

    fun setShopClear(projectId: String, stamp: String) {
        prefs.edit().putString(KEY_SHOP_CLEAR + projectId, stamp).apply()
    }

    companion object {
        private const val KEY_SERVER = "server_url"
        private const val KEY_TOKEN = "device_token"
        private const val KEY_ACCOUNT = "account_id"
        private const val KEY_ACCOUNT_NAME = "account_name"
        private const val KEY_INBOX = "inbox_id"
        private const val KEY_SHOP_CLEAR = "shop_clear_"
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
