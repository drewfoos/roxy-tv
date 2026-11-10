package tv.roxy.tv.player

/**
 * Where the next-episode card appears, in seconds into the file. It depends on what follows
 * the credits:
 *  - an outro running to the end of the file: at the outro's start (Play now = skip the
 *    credits); with auto-skip never, since the skipped outro reaches the end by itself;
 *  - something after the outro (a post-credits scene, an epilogue): at the next-episode
 *    preview, or the last 15 s when there is none;
 *  - no markers at all: the last 15 s.
 */
internal fun nextCardTrigger(outroStartSec: Double?, outroEndSec: Double?, previewStartSec: Double?, durationSec: Double, autoSkip: Boolean): Double {
    val outroEndsFile = outroStartSec != null && outroEndSec != null && outroEndSec >= durationSec - 5.0
    return when {
        outroEndsFile -> if (autoSkip) Double.MAX_VALUE else outroStartSec!!
        previewStartSec != null -> previewStartSec
        else -> durationSec - 15.0
    }
}

/** What to do with a next-episode card that is up (see [nextCardStep]). */
internal enum class NextCardStep { Keep, Hide, StopCountdown }

/** How far before its trigger the position has to be for the card to go: a seek, not a wobble. */
internal const val NEXT_CARD_BACK_SEC = 1.0

/**
 * A card that is up, looked at again on every refresh. Seeking back to before the point
 * where it appeared takes it down, countdown and all: it used to keep counting and then
 * play the next episode from the middle of this one. That is not a dismissal, so the card
 * comes back when the position reaches the trigger again. Autoplay turned off while it
 * counts stops the countdown and leaves the card to wait for a choice.
 */
internal fun nextCardStep(cardUp: Boolean, countingDown: Boolean, autoplay: Boolean, positionSec: Double, triggerAtSec: Double): NextCardStep = when {
    !cardUp -> NextCardStep.Keep
    positionSec < triggerAtSec - NEXT_CARD_BACK_SEC -> NextCardStep.Hide
    countingDown && !autoplay -> NextCardStep.StopCountdown
    else -> NextCardStep.Keep
}
