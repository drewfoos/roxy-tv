package tv.roxy.tv.player

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.media.AudioAttributes
import android.media.AudioFocusRequest
import android.media.AudioManager
import android.media.MediaMetadata
import android.media.session.MediaSession
import android.media.session.PlaybackState
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import androidx.core.content.ContextCompat
import timber.log.Timber

/**
 * What the system needs from a player, which mpv does not do on its own:
 *  - a MediaSession, so Assistant ("pause", "skip forward"), HDMI-CEC and Bluetooth remotes
 *    and the system's now-playing surfaces reach the player;
 *  - audio focus, so another app's sound pauses or ducks us (and ours pauses theirs);
 *  - "becoming noisy": headphones unplugged or a Bluetooth headset gone pauses playback.
 * Every callback arrives on the main thread.
 */
class PlaybackGlue(context: Context, private val controls: Controls) {

    interface Controls {
        fun play()
        fun pause()
        fun seekTo(seconds: Double)
        fun seekBy(seconds: Int)
        fun next()
        fun stop()
        /** Another app wants to speak briefly (navigation, a notification): lower the volume meanwhile. */
        fun duck(on: Boolean)
        fun isPlaying(): Boolean
    }

    private val app = context.applicationContext
    private val main = Handler(Looper.getMainLooper())
    private val audio = app.getSystemService(AudioManager::class.java)
    private var released = false

    private val media = MediaSession(app, "Roxy TV").apply {
        setCallback(object : MediaSession.Callback() {
            override fun onPlay() = controls.play()
            override fun onPause() = controls.pause()
            override fun onSeekTo(pos: Long) = controls.seekTo(pos / 1000.0)
            override fun onFastForward() = controls.seekBy(10)
            override fun onRewind() = controls.seekBy(-10)
            override fun onSkipToNext() = controls.next()
            override fun onStop() = controls.stop()
        }, main)
        isActive = true
    }

    // ---- audio focus ----

    private var hasFocus = false
    /** Paused because another app took focus for a while; resume when it gives it back. */
    private var pausedByLoss = false

    private val focusRequest: AudioFocusRequest = AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN)
        .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA).setContentType(AudioAttributes.CONTENT_TYPE_MOVIE).build())
        .setWillPauseWhenDucked(false)
        .setOnAudioFocusChangeListener({ change ->
            if (released) return@setOnAudioFocusChangeListener
            when (change) {
                AudioManager.AUDIOFOCUS_LOSS -> { hasFocus = false; pausedByLoss = false; controls.pause() }
                AudioManager.AUDIOFOCUS_LOSS_TRANSIENT -> { if (controls.isPlaying()) pausedByLoss = true; controls.pause() }
                AudioManager.AUDIOFOCUS_LOSS_TRANSIENT_CAN_DUCK -> controls.duck(true)
                AudioManager.AUDIOFOCUS_GAIN -> {
                    hasFocus = true
                    controls.duck(false)
                    if (pausedByLoss) { pausedByLoss = false; controls.play() }
                }
            }
        }, main)
        .build()

    /** Ask for focus when playback starts (mpv-android re-asks on every unpause too). */
    private fun requestFocus() {
        if (hasFocus || audio == null) return
        hasFocus = audio.requestAudioFocus(focusRequest) == AudioManager.AUDIOFOCUS_REQUEST_GRANTED
        if (!hasFocus) Timber.i("audio focus not granted")
    }

    // ---- becoming noisy ----

    private val noisy = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            if (intent.action == AudioManager.ACTION_AUDIO_BECOMING_NOISY && !released) controls.pause()
        }
    }

    init {
        // System broadcasts still reach a not-exported receiver.
        ContextCompat.registerReceiver(app, noisy, IntentFilter(AudioManager.ACTION_AUDIO_BECOMING_NOISY), ContextCompat.RECEIVER_NOT_EXPORTED)
    }

    // ---- state ----

    private var lastMeta: String? = null

    /** Publish what plays and where it is; the system extrapolates the position from [positionSec] and [speed]. */
    fun update(title: String, subtitle: String, durationSec: Double, positionSec: Double, playing: Boolean, buffering: Boolean, speed: Double, hasNext: Boolean) {
        if (released) return
        if (playing) requestFocus()
        val metaKey = "$title|$subtitle|${durationSec.toLong()}"
        if (metaKey != lastMeta) {
            lastMeta = metaKey
            media.setMetadata(
                MediaMetadata.Builder()
                    .putString(MediaMetadata.METADATA_KEY_TITLE, title)
                    .putString(MediaMetadata.METADATA_KEY_DISPLAY_TITLE, title)
                    .putString(MediaMetadata.METADATA_KEY_DISPLAY_SUBTITLE, subtitle)
                    .putLong(MediaMetadata.METADATA_KEY_DURATION, (durationSec * 1000).toLong())
                    .build(),
            )
        }
        var actions = PlaybackState.ACTION_PLAY or PlaybackState.ACTION_PAUSE or PlaybackState.ACTION_PLAY_PAUSE or
            PlaybackState.ACTION_SEEK_TO or PlaybackState.ACTION_FAST_FORWARD or PlaybackState.ACTION_REWIND or PlaybackState.ACTION_STOP
        if (hasNext) actions = actions or PlaybackState.ACTION_SKIP_TO_NEXT
        val state = when {
            buffering && playing -> PlaybackState.STATE_BUFFERING
            playing -> PlaybackState.STATE_PLAYING
            else -> PlaybackState.STATE_PAUSED
        }
        media.setPlaybackState(
            PlaybackState.Builder()
                .setActions(actions)
                .setState(state, (positionSec * 1000).toLong(), if (state == PlaybackState.STATE_PLAYING) speed.toFloat() else 0f, SystemClock.elapsedRealtime())
                .build(),
        )
    }

    fun release() {
        if (released) return
        released = true
        runCatching { app.unregisterReceiver(noisy) }
        runCatching { audio?.abandonAudioFocusRequest(focusRequest) }
        runCatching { media.isActive = false; media.release() }
    }
}
