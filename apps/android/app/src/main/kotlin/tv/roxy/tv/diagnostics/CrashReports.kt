package tv.roxy.tv.diagnostics

import android.content.Context
import android.os.Build
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import timber.log.Timber
import tv.roxy.tv.BuildConfig
import tv.roxy.tv.api.RoxyApi
import java.io.File

/**
 * Keeps the last crash: an uncaught exception's stack trace is written to a file before
 * the process dies, and the next launch sends it to the server (POST /v1/diagnostics/crash,
 * which logs it) and deletes it. Nothing leaves the home network.
 */
object CrashReports {
    private fun file(context: Context) = File(context.filesDir, "last-crash.txt")

    fun install(context: Context) {
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, error ->
            runCatching {
                file(context).writeText(
                    "Roxy ${BuildConfig.VERSION_NAME} (${BuildConfig.VERSION_CODE}) ${BuildConfig.APPLICATION_ID}\n" +
                        "${Build.MANUFACTURER} ${Build.MODEL} Android ${Build.VERSION.RELEASE} (API ${Build.VERSION.SDK_INT})\n" +
                        "thread ${thread.name}\n\n" + error.stackTraceToString().take(60_000),
                )
            }
            previous?.uncaughtException(thread, error)
        }
    }

    /** Sends a crash saved by an earlier run, once the app knows its server. */
    fun sendPending(context: Context, api: RoxyApi, scope: CoroutineScope) {
        val f = file(context)
        if (!f.exists()) return
        scope.launch(Dispatchers.IO) {
            val text = runCatching { f.readText() }.getOrNull() ?: return@launch
            runCatching { api.reportCrash(text) }
                .onSuccess { f.delete(); Timber.i("crash report sent") }
                .onFailure { Timber.w("crash report not sent: %s", it.message) }
        }
    }
}
