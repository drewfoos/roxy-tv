import { Link } from 'react-router'
import { api, px } from '../api/client'
import { runtimeLabel } from '../api/items'
import { useAsync } from '../hooks/useAsync'
import { Art, Row } from './Cards'
import { IconPlay } from './Icons'

const KIND: Record<string, string> = {
  trailer: 'Trailer',
  featurette: 'Featurette',
  behindthescenes: 'Behind the scenes',
  deleted: 'Deleted scene',
  interview: 'Interview',
  scene: 'Scene',
  short: 'Short',
  clip: 'Clip',
  theme: 'Theme video',
  extra: 'Extra',
}

/** A film's or show's extras (trailers, featurettes, deleted scenes...), each opening the player. Nothing when it has none. */
export function ExtrasRow({ id }: { id: string }) {
  const { data } = useAsync(`extras:${id}`, () => api.extras(id).catch(() => ({ items: [] })), [id])
  const items = data?.items ?? []
  if (items.length === 0) return null
  return (
    <Row title="Extras" thumbs>
      {items.map((x) => (
        <div key={x.id} className="card">
          <Link to={`/watch/${x.id}`} className="card__art card__art--thumb">
            <Art url={api.imageUrl(x, 'thumb', px(420))} title={x.title} />
            <div className="play-ring">
              <IconPlay size={28} />
            </div>
            {runtimeLabel(x) && <span className="badge badge--br">{runtimeLabel(x)}</span>}
          </Link>
          <div className="card__name">{x.title}</div>
          <div className="card__sub">
            <span>{KIND[x.extraType ?? ''] ?? 'Extra'}</span>
          </div>
        </div>
      ))}
    </Row>
  )
}
