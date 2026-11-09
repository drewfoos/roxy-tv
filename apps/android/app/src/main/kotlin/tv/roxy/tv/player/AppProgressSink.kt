package tv.roxy.tv.player

import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.ApiException

/** [ProgressReporter.Sink] on the app's API and offline queue. */
class AppProgressSink(private val app: RoxyApp) : ProgressReporter.Sink {
    override suspend fun progress(r: ProgressReporter.Report) =
        app.api.progress(r.itemId, r.positionMs, r.audioIndex, r.subtitleIndex, ageMs = r.ageMs, sourceId = r.sourceId, durationMs = r.durationMs)
    override suspend fun markPlayed(itemId: String) = app.api.setPlayed(itemId, true, ageMs = 0)
    override fun isDownload(itemId: String) = app.downloads.entry(itemId) != null
    override fun hasPending(itemId: String) = app.downloads.hasPendingProgress(itemId)
    override suspend fun syncPending() = app.downloads.syncProgress(app.api)
    override fun queue(r: ProgressReporter.Report, sent: Boolean) =
        app.downloads.recordProgress(r.itemId, r.positionMs, reportedToServer = sent, audioIndex = r.audioIndex, subtitleIndex = r.subtitleIndex, ageMs = r.ageMs, sourceId = r.sourceId, durationMs = r.durationMs)
    override fun queuePlayed(itemId: String, sent: Boolean) = app.downloads.recordProgress(itemId, 0, reportedToServer = sent, played = true)
    override fun userDataChanged() = app.items.userDataChanged()
    override fun answered(t: Throwable) = t is ApiException
}
