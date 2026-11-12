import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { AddressSettings } from '../../api/admin'
import { useAsync } from '../../hooks/useAsync'
import { ActionButton, Facts, Field, Notice, PanelHead, TextArea, errorText } from '../../components/Form'
import { LoadError } from '../../components/Connection'
import { Spinner } from '../../components/Skeleton'

type Check = NonNullable<AddressSettings['checks']>[number]

/** One address per line, as typed: blank lines and the spaces around an address do not count. */
export function addressLines(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter((l) => l !== '')
}

/** What to tell the admin about one address after a save. */
export function checkWords(c: Check): { kind: 'ok' | 'info'; text: string } {
  if (c.state === 'ok') return { kind: 'ok', text: `${c.address} reaches this server.` }
  const why = c.state === 'notRoxy' ? 'something else answered there' : 'nothing answered there'
  return { kind: 'info', text: `${c.address} is saved, but the server could not confirm it from where it runs (${why}). That can be normal for an address meant for outside your home. The apps check it themselves before they use it.` }
}

/**
 * Server addresses (admin): the other ways to reach this server, like its Tailscale
 * address or a domain. Signed-in phones and TVs are told them and use whichever answers,
 * so nobody has to sign in again when they leave the house.
 */
export function AddressesPage() {
  const { data: latest, error, loading, fresh, reload } = useAsync('server-addresses', () => api.addresses())
  // Filled and saved only from the server's own answer, never a cached copy of an earlier visit.
  const data = fresh ? latest : null
  const [text, setText] = useState<string | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [checks, setChecks] = useState<Check[]>([])
  useEffect(() => {
    if (data && text == null) setText(data.addresses.join('\n'))
  }, [data, text])

  const save = async () => {
    setErr(null)
    setChecks([])
    try {
      const saved = await api.saveAddresses(addressLines(text ?? ''))
      setText(saved.addresses.join('\n'))
      setChecks(saved.checks ?? [])
    } catch (e) {
      setErr(errorText(e))
      return false
    }
  }

  return (
    <section className="acc-form">
      <PanelHead title="Server addresses" sub="Other ways to reach this server, like its Tailscale address. Signed-in phones and TVs learn them and use whichever answers: the home address at home, another one when away." />
      {!data && error ? (
        <LoadError error={error} onRetry={() => reload()} />
      ) : !data || (loading && text == null) ? (
        <Spinner />
      ) : (
        <>
          {data.advertised && <Facts rows={[['Home address', data.advertised]]} />}
          {data.fromEnvironment ? (
            <>
              <Notice kind="info">These are set by ROXY_OTHER_URLS where the server is started. Change them there, or remove that setting to edit them here.</Notice>
              <Facts rows={data.addresses.map((a, i) => [`Address ${i + 1}`, a])} />
            </>
          ) : (
            <>
              <Field label="Other addresses" hint="One per line, for example http://100.64.0.7:8097 or https://roxy.example.com. Plain http is only for a home-network or Tailscale IP address, or a .ts.net name; anything else needs https.">
                <TextArea value={text ?? ''} onChange={setText} rows={4} placeholder="http://100.64.0.7:8097" />
              </Field>
              {err && <Notice kind="error">{err}</Notice>}
              {checks.map((c) => {
                const w = checkWords(c)
                return (
                  <Notice key={c.address} kind={w.kind}>
                    {w.text}
                  </Notice>
                )
              })}
              <div className="acc-actions">
                <ActionButton onClick={save} done="Saved">
                  Save addresses
                </ActionButton>
              </div>
            </>
          )}
        </>
      )}
    </section>
  )
}
