package tv.roxy.tv.player

import tv.roxy.tv.RoxyApp
import tv.roxy.tv.api.ApiException

/** [ProgressReporter.Sink] on the app's durable progress queue (Downloads) and API. */
class AppProgressSink(private val app: RoxyApp) : ProgressReporter.Sink {
    // The profile that opened the player: its reports name it to the end (Downloads.recordProgress).
    private val profile = app.prefs.session.value.profileId
    // A stop or end report and a watched mark are on disk before the call returns (a kill
    // right after must not lose them); ticks and pauses are written moments later.
    override fun persist(r: ProgressReporter.Report) {
        app.downloads.recordProgress(r.itemId, r.positionMs, reportedToServer = false, audioIndex = r.audioIndex, subtitleIndex = r.subtitleIndex, ageMs = r.ageMs, sourceId = r.sourceId, durationMs = r.durationMs,
            durableNow = r.reason == "stop" || r.reason == "end", profile = profile)
    }
    override fun persistPlayed(itemId: String) { app.downloads.recordProgress(itemId, 0, reportedToServer = false, played = true, durableNow = true, profile = profile) }
    override suspend fun drain(): Throwable? = app.downloads.syncProgress(app.api)
    override fun userDataChanged() = app.items.userDataChanged()
    override fun answered(t: Throwable) = t is ApiException
}
