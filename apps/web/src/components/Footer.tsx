import { Link } from 'react-router'
import { APP_VERSION, api } from '../api/client'
import { useAsync } from '../hooks/useAsync'
import { signOut } from './TopBar'
import { WitchHat } from './Icons'

/**
 * Crunchyroll's footer: the mark with a line about the service at the left, then
 * Explore / Resources / Account columns, and a thin legal strip underneath.
 */
export function Footer() {
  const { data: info } = useAsync('system-info', () => api.systemInfo())
  return (
    <footer className="footer">
      <div className="footer__main">
        <div className="footer__brand">
          <div className="footer__logo">
            <WitchHat size={36} />
            <span>Roxy</span>
          </div>
          <div className="footer__line">
            <span className="footer__server">{info?.name ?? 'Roxy Server'}</span>
            <span className="footer__sep">|</span>
            <span>Roxy TV</span>
          </div>
          <p className="footer__tag">Your anime, your server: styled subtitles, Hi10P and 10-bit HEVC played as they are, nothing re-encoded.</p>
        </div>
        <div className="footer__col">
          <h4>Explore</h4>
          <Link to="/">Home</Link>
          <Link to="/discover">Discover</Link>
          <Link to="/browse?c=series">Browse Series</Link>
          <Link to="/browse?c=movies">Browse Movies</Link>
        </div>
        <div className="footer__col">
          <h4>Resources</h4>
          <Link to="/legal/terms">Terms of Use</Link>
          <Link to="/legal/privacy">Privacy Policy</Link>
          <a href="https://github.com/drewfoos/roxy-tv" target="_blank" rel="noreferrer">
            Source on GitHub
          </a>
        </div>
        <div className="footer__col">
          <h4>Account</h4>
          <Link to="/profiles">Switch Profile</Link>
          <Link to="/lists">Watchlist</Link>
          <Link to="/lists?tab=history">History</Link>
          <Link to="/account">My Account</Link>
          <button onClick={signOut}>Log Out</button>
        </div>
      </div>
      <div className="footer__legal">
        <Link to="/legal/terms">Terms of Use</Link>
        <Link to="/legal/privacy">Privacy Policy</Link>
        <span>
          Roxy Web {APP_VERSION}
          {info?.version ? ` · Server ${info.version}` : ''}
        </span>
      </div>
    </footer>
  )
}
