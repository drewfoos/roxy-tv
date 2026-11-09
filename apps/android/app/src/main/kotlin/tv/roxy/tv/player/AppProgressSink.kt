package tv.roxy.tv.player

import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.ApiException

/** [ProgressReporter.Sink] on the app's durable progress queue (Downloads) and API. */
class AppProgressSink(private val app: RoxyApp) : ProgressReporter.Sink {
    override fun persist(r: ProgressReporter.Report) =
        app.downloads.recordProgress(r.itemId, r.positionMs, reportedToServer = false, audioIndex = r.audioIndex, subtitleIndex = r.subtitleIndex, ageMs = r.ageMs, sourceId = r.sourceId, durationMs = r.durationMs)
    override fun persistPlayed(itemId: String) = app.downloads.recordProgress(itemId, 0, reportedToServer = false, played = true)
    override suspend fun drain(): Throwable? = app.downloads.syncProgress(app.api)
    override fun userDataChanged() = app.items.userDataChanged()
    override fun answered(t: Throwable) = t is ApiException
}
