import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import { useAsync } from '../../hooks/useAsync'
import { ActionButton, Notice, PanelHead, RadioList, errorText } from '../../components/Form'
import { Spinner } from '../../components/Skeleton'

const HINTS: Record<string, string> = {
  auto: 'Recommended: uses the best encoder this server has',
  software: 'Works everywhere; slower and uses more power',
}

/** Transcoding (admin): which encoder converts video for the lower qualities. */
export function TranscodingPage() {
  const { data, error, loading, reload } = useAsync('transcoding', () => api.transcoding())
  const [choice, setChoice] = useState<string | null>(null)
  const [err, setErr] = useState<string | null>(null)
  useEffect(() => {
    if (data && choice == null) setChoice(data.hwAccel)
  }, [data, choice])
  const save = async () => {
    setErr(null)
    try {
      await api.saveTranscoding(choice ?? 'auto')
      reload(true)
    } catch (e) {
      setErr(errorText(e))
    }
  }
  return (
    <section className="acc-form">
      <PanelHead title="Transcoding" sub="Which GPU converts video when a player asks for a lower quality than the file. Direct play never transcodes." />
      {error && <Notice kind="error">{error}</Notice>}
      {loading && !data ? (
        <Spinner />
      ) : data ? (
        <>
          <Notice>
            In use now: <b>{data.encoder || data.hwAccel}</b>
          </Notice>
          <RadioList
            value={choice ?? data.hwAccel}
            options={data.options.map((o) => ({ id: o.id, label: o.label, hint: o.available ? HINTS[o.id] : 'Not found on this server', disabled: !o.available }))}
            onChange={setChoice}
          />
          {err && <Notice kind="error">{err}</Notice>}
          <div className="acc-actions">
            <ActionButton onClick={save} done="Saved" disabled={choice === data.hwAccel}>
              Save
            </ActionButton>
          </div>
          <p className="acc-foot">Videos already converting keep their encoder; the next one uses this.</p>
        </>
      ) : null}
    </section>
  )
}
