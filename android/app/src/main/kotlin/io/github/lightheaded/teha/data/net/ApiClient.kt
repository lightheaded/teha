// SPDX-License-Identifier: Apache-2.0

package io.github.lightheaded.teha.data.net

import io.github.lightheaded.teha.data.Settings
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.IOException
import java.util.concurrent.TimeUnit

/** ApiError carries a message that the user can act on. */
class ApiError(message: String, val unauthorized: Boolean = false) : IOException(message)

/**
 * ApiClient speaks the two routes the app needs.
 *
 * The token goes on every request. A missing or wrong token returns 401 with a
 * WWW-Authenticate header, and the caller turns that into one clear line about
 * the token rather than a generic network failure.
 */
class ApiClient(private val settings: Settings) {

    private val http = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .writeTimeout(30, TimeUnit.SECONDS)
        .retryOnConnectionFailure(true)
        .build()

    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = false
        explicitNulls = false
        // A null where a list is declared uses the default instead of failing
        // the whole parse. Go marshals a nil slice to `null`, and a server that
        // does so once broke every answer, not just that field:
        //   Expected start of the array '[', but had 'n' instead at path: $.applied
        // The server is fixed. This keeps an older server working as well.
        coerceInputValues = true
    }

    val jsonFormat: Json get() = json

    suspend fun health(): Long = withContext(Dispatchers.IO) {
        val body = call(Request.Builder().url(url("/v1/health")).get())
        json.decodeFromString(HealthResponse.serializer(), body).version
    }

    suspend fun sync(req: SyncRequest): SyncResponse = withContext(Dispatchers.IO) {
        val payload = json.encodeToString(SyncRequest.serializer(), req)
        val body = call(
            Request.Builder()
                .url(url("/v1/sync"))
                .post(payload.toRequestBody(JSON_MEDIA))
        )
        json.decodeFromString(SyncResponse.serializer(), body)
    }

    private fun url(path: String): String {
        val base = settings.serverUrl
        if (base.isEmpty()) throw ApiError("Set the server address in settings.")
        if (!base.startsWith("http://") && !base.startsWith("https://")) {
            throw ApiError("The server address must start with http:// or https://.")
        }
        return base + path
    }

    private fun call(builder: Request.Builder): String {
        val token = settings.token
        if (token.isNotEmpty()) builder.header("Authorization", "Bearer $token")
        builder.header("Accept", "application/json")
        http.newCall(builder.build()).execute().use { response ->
            val text = response.body?.string().orEmpty()
            if (response.code == 401) {
                throw ApiError("The server refused the token.", unauthorized = true)
            }
            if (!response.isSuccessful) {
                throw ApiError("The server answered ${response.code}. ${shorten(text)}")
            }
            return text
        }
    }

    private fun shorten(s: String): String =
        if (s.length <= 200) s else s.take(200) + "..."

    private companion object {
        val JSON_MEDIA = "application/json; charset=utf-8".toMediaType()
    }
}
