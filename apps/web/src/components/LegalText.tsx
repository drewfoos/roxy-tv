import { Fragment, type ReactNode } from 'react'

/**
 * The server's legal documents are plain text with blank lines between paragraphs and
 * **bold** lead-ins. Render exactly that: paragraphs, and bold where it is marked.
 */
export function LegalText({ body }: { body: string }) {
  const paragraphs = body.split(/\n\s*\n/).map((p) => p.trim()).filter(Boolean)
  return (
    <div className="legal-text">
      {paragraphs.map((p, i) => (
        <p key={i}>{withBold(p)}</p>
      ))}
    </div>
  )
}

function withBold(text: string): ReactNode {
  const parts = text.split(/\*\*(.+?)\*\*/g)
  return parts.map((part, i) => (i % 2 === 1 ? <b key={i}>{part}</b> : <Fragment key={i}>{part}</Fragment>))
}
