/** Shimmer placeholders shaped like the content (never the word "Loading"). */
export function RowSkeleton({ thumbs = false }: { thumbs?: boolean }) {
  return (
    <section className="row">
      <div className="row__head">
        <div className="bone bone--line" style={{ width: 180, height: 20 }} />
      </div>
      <div className={'row__track' + (thumbs ? ' row__track--thumbs' : '')}>
        {Array.from({ length: 8 }).map((_, i) => (
          <div key={i} className="bone" style={{ aspectRatio: thumbs ? '16 / 9' : '2 / 3', borderRadius: 8 }} />
        ))}
      </div>
    </section>
  )
}

export function GridSkeleton({ count = 18, thumbs = false }: { count?: number; thumbs?: boolean }) {
  return (
    <div className={'grid' + (thumbs ? ' grid--thumbs' : '')}>
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="bone" style={{ aspectRatio: thumbs ? '16 / 9' : '2 / 3', borderRadius: 8 }} />
      ))}
    </div>
  )
}

export function Spinner() {
  return (
    <div className="center">
      <div className="spinner" />
    </div>
  )
}
