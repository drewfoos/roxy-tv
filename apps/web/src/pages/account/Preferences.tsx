import { useSession } from '../../app/useSession'
import { LANGUAGES, QUALITY_PREFS, SKIP_WINDOWS, SUB_MODES, prefs, usePrefs } from '../../api/prefs'
import { Field, PanelHead, RadioList, SectionHead, Select, ToggleRow } from '../../components/Form'

/** Playback preferences (the phone's PLAYBACK group), kept in this browser. */
export function PreferencesPage() {
  const s = useSession()
  const p = usePrefs()
  const langs = LANGUAGES.map((l) => ({ id: l.code, label: l.name }))
  return (
    <section className="acc-form">
      <PanelHead title={`Preferences for ${s?.profileName || s?.userName || 'you'}`} sub="Set your language and video preferences. They apply to this browser; the phone and TV apps keep their own." />

      <SectionHead>Video</SectionHead>
      <Field label="Audio Language" hint="Picked when a title has more than one audio track. Your last choice on a title still wins.">
        <Select value={p.audioLang} options={langs} onChange={(v) => prefs.set({ audioLang: v })} />
      </Field>
      <Field label="Subtitles/CC Language">
        <Select value={p.subLang} options={langs} onChange={(v) => prefs.set({ subLang: v })} />
      </Field>
      <div className="ffield">
        <span className="ffield__label">Subtitles</span>
        <RadioList value={p.subMode} options={SUB_MODES} onChange={(v) => prefs.set({ subMode: v })} />
      </div>
      <Field label="Video Quality" hint="The file plays as it is unless you pick something lower; the player's gear menu changes it for one episode.">
        <Select value={p.quality} options={QUALITY_PREFS} onChange={(v) => prefs.set({ quality: v })} />
      </Field>

      <SectionHead>Playback</SectionHead>
      <ToggleRow title="Autoplay Next Episode" hint="The next episode starts when one ends." on={p.autoplay} onChange={(v) => prefs.set({ autoplay: v })} />
      <ToggleRow title="Auto-skip Intro & Outro" hint="Jumps past openings and endings the server has found. Off shows a Skip button instead." on={p.autoSkip} onChange={(v) => prefs.set({ autoSkip: v })} />
      {!p.autoSkip && (
        <Field label="Skip button stays for">
          <Select value={p.skipWindowSec} options={SKIP_WINDOWS} onChange={(v) => prefs.set({ skipWindowSec: v })} />
        </Field>
      )}
    </section>
  )
}
