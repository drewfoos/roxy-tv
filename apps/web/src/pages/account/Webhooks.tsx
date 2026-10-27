import { useState } from 'react'
import { api } from '../../api/client'
import { useAsync } from '../../hooks/useAsync'
import { ActionButton, ListRow, Notice, PanelHead, SectionHead, errorText, formatWhen } from '../../components/Form'
import { IconCopy, IconDelete } from '../../components/Icons'

function CopyRow({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1400)
    } catch {
      /* no clipboard: the text is selectable */
    }
  }
  return (
    <div className="copyrow">
      <span className="ffield__label">{label}</span>
      <div className="copyrow__line">
        <code>{value}</code>
        <button type="button" className="icon-btn" onClick={copy} aria-label={`Copy ${label}`}>
          {copied ? 'Copied' : <IconCopy size={18} />}
        </button>
      </div>
    </div>
  )
}

/** Webhooks (admin): the URLs Sonarr and Radarr call, the keys they use, and what arrived. */
export function WebhooksPage() {
  const keys = useAsync('api-keys', () => api.apiKeys())
  const events = useAsync('webhook-events', () => api.webhookEvents(30))
  const [fresh, setFresh] = useState<{ name: string; token: string } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const base = window.location.origin

  const make = async (name: string) => {
    setError(null)
    try {
      const k = await api.createApiKey(name)
      setFresh({ name: k.name, token: k.token })
      keys.reload(true)
    } catch (e) {
      setError(errorText(e))
    }
  }
  const drop = async (id: string) => {
    setError(null)
    try {
      await api.deleteApiKey(id)
      keys.reload(true)
    } catch (e) {
      setError(errorText(e))
    }
  }

  return (
    <section className="acc-form">
      <PanelHead title="Webhooks" sub="How imports reach the library without a rescan. In Sonarr and Radarr add a Webhook connection with one of these URLs and a key as the header X-Api-Key." />

      <SectionHead>URLs</SectionHead>
      <CopyRow label="Sonarr" value={`${base}/v1/webhooks/sonarr`} />
      <CopyRow label="Radarr" value={`${base}/v1/webhooks/radarr`} />

      <SectionHead>API keys</SectionHead>
      {error && <Notice kind="error">{error}</Notice>}
      {fresh && (
        <div className="notice notice--ok">
          <b>New key for {fresh.name} (shown once, copy it now)</b>
          <CopyRow label="Key" value={fresh.token} />
        </div>
      )}
      <div className="acc-list">
        {(keys.data?.keys ?? []).map((k) => (
          <ListRow
            key={k.id}
            title={k.name}
            sub={k.createdAt ? `Created ${formatWhen(k.createdAt)}` : undefined}
            right={
              <button type="button" className="icon-btn" onClick={() => void drop(k.id)} aria-label={`Delete key ${k.name}`}>
                <IconDelete size={20} />
              </button>
            }
          />
        ))}
        {keys.data && keys.data.keys.length === 0 && <Notice>No keys yet.</Notice>}
      </div>
      <div className="acc-actions">
        <ActionButton onClick={() => make('Sonarr')}>New key for Sonarr</ActionButton>
        <ActionButton kind="grey" onClick={() => make('Radarr')}>
          For Radarr
        </ActionButton>
      </div>

      <SectionHead>Recent notifications</SectionHead>
      <div className="acc-list">
        {(events.data?.events ?? []).map((e, i) => (
          <ListRow
            key={e.id ?? i}
            title={`${e.source} · ${e.eventType}`}
            sub={
              <>
                {e.title && <div>{e.title}</div>}
                {e.result && <div>{e.result}</div>}
                <div>{formatWhen(e.at ?? e.receivedAt)}</div>
              </>
            }
          />
        ))}
        {events.data && events.data.events.length === 0 && <Notice>Nothing has arrived yet.</Notice>}
      </div>
    </section>
  )
}
