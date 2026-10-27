import { Link } from 'react-router'
import { api, px } from '../../api/client'
import { LANGUAGES, QUALITY_PREFS, SUB_MODES, usePrefs } from '../../api/prefs'
import { useAsync } from '../../hooks/useAsync'
import { useSession } from '../../app/useSession'
import { Avatar } from '../../components/TopBar'
import { IconChevronRight, IconHistory, IconLogout, IconPerson, IconSettings, IconSubtitles, IconSwap } from '../../components/Icons'
import type { ReactNode } from 'react'

/**
 * The phone app's Account tab (PhoneAccount.kt), shown at /account under 601 px: the
 * profile card, then grouped cards of rows with a blue icon, label, value and chevron.
 * Each row opens the matching settings page.
 */
export function PhoneAccountIndex() {
  const s = useSession()
  const p = usePrefs()
  const { data: info } = useAsync('system-info', () => api.systemInfo())
  const { data: reqs } = useAsync(s?.isAdmin ? 'requests-admin' : null, () => api.requests())
  if (!s) return null
  const avatar = s.profileId && s.profileHasAvatar ? api.profileAvatarUrl(s.profileId, px(60) * 2, s.profileAvatarKey) : null
  const langName = (c: string) => LANGUAGES.find((l) => l.code === c)?.name ?? c
  const subMode = SUB_MODES.find((m) => m.id === p.subMode)
  const waiting = reqs?.waiting ?? 0
  return (
    <div className="pacc">
      <h1 className="pacc__title">Account</h1>
      <Link to="/account/profiles" className="pacc__card pacc__profile">
        <Avatar name={s.profileName || s.userName} url={avatar} size={60} />
        <span className="pacc__ptext">
          <b>{s.profileName || s.userName}</b>
          <span>
            {s.userName} · {info?.name ?? window.location.host}
          </span>
          <em>Edit profile</em>
        </span>
        <IconChevronRight size={24} />
      </Link>

      <Group>
        <Row to="/profiles" icon={<IconSwap size={22} />} label="Switch profile" value={'Back to "Who’s watching?"'} />
        <Row to="/account/password" icon={<IconSettings size={22} />} label="Change password" value={`For ${s.userName}; signs out your other devices`} />
      </Group>

      <Group title="Playback">
        <Row to="/account/preferences" icon={<IconSubtitles size={22} />} label="Audio language" value={langName(p.audioLang)} />
        <Row to="/account/preferences" icon={<IconSubtitles size={22} />} label="Subtitle language" value={langName(p.subLang)} />
        <Row to="/account/preferences" icon={<IconSubtitles size={22} />} label="Subtitles" value={subMode ? `${subMode.label} · ${subMode.hint}` : ''} />
        <Row to="/account/preferences" icon={<IconSettings size={22} />} label="Video quality" value={QUALITY_PREFS.find((q) => q.id === p.quality)?.label ?? ''} />
        <Row to="/account/preferences" icon={<IconSettings size={22} />} label="Auto-skip intro & outro" value={p.autoSkip ? 'On' : 'Off'} />
        <Row to="/account/preferences" icon={<IconSettings size={22} />} label="Autoplay next episode" value={p.autoplay ? 'On' : 'Off'} />
      </Group>

      <Group title="Devices">
        <Row to="/account/devices" icon={<IconHistory size={22} />} label="Device Management" value="The apps and browsers on your login" />
      </Group>

      {s.isAdmin && (
        <Group title="Server">
          <Row to="/account/requests" icon={<IconPerson size={22} />} label="Requests" value={waiting > 0 ? `${waiting} waiting for your approval` : 'Every request, approvals, downloads and problems'} />
          <Row to="/account/users" icon={<IconPerson size={22} />} label="Users" value="Accounts for other people: admin, requester or basic" />
          <Row to="/account/library" icon={<IconSettings size={22} />} label="Library" value="Scan, refresh metadata, fix matches and custom shows" />
          <Row to="/account/arr" icon={<IconSettings size={22} />} label="Sonarr & Radarr" value="Where Discover requests go" />
          <Row to="/account/transcoding" icon={<IconSettings size={22} />} label="Transcoding" value="Which GPU converts video for lower qualities" />
          <Row to="/account/keys" icon={<IconSettings size={22} />} label="TMDB & fanart.tv" value="Titles, posters, logos and backgrounds" />
          <Row to="/account/webhooks" icon={<IconSettings size={22} />} label="Webhooks" value="How imports reach the library without a rescan" />
        </Group>
      )}

      <Group title="About">
        <Row to="/account/about" icon={<IconSettings size={22} />} label="About Roxy" value={`Roxy Web · ${info?.name ?? ''}${info?.version ? ` ${info.version}` : ''}`} />
        <Row to="/legal/terms" icon={<IconSettings size={22} />} label="Terms of Use" value="Read the terms" />
        <Row to="/legal/privacy" icon={<IconSettings size={22} />} label="Privacy" value="What stays on your server and your device" />
      </Group>

      <Link to="/account/logout" className="pacc__card pacc__logout">
        <IconLogout size={20} /> Log out
      </Link>
      <div className="pacc__signed">Signed in as {s.userName}</div>
    </div>
  )
}

function Group({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <>
      {title ? <div className="pacc__head">{title}</div> : <div style={{ height: 10 }} />}
      <div className="pacc__card">{children}</div>
    </>
  )
}

function Row({ to, icon, label, value }: { to: string; icon: ReactNode; label: string; value?: string }) {
  return (
    <Link to={to} className="pacc__row">
      <span className="pacc__icon">{icon}</span>
      <span className="pacc__text">
        <span>{label}</span>
        {value && <small>{value}</small>}
      </span>
      <IconChevronRight size={24} />
    </Link>
  )
}
