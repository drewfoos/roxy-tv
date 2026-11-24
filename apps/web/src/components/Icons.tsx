// Inline Material Symbols (24 px grid) so the app needs no font or icon CDN: it must
// work on a server with no internet. Paths are Material Design icons (Apache 2.0).
import { useId } from 'react'
import type { SVGProps } from 'react'

type P = SVGProps<SVGSVGElement> & { size?: number }

const Svg = ({ size = 24, children, ...rest }: P) => (
  <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" {...rest}>
    {children}
  </svg>
)

export const IconHome = (p: P) => <Svg {...p}><path d="M10 20v-6h4v6h5v-8h3L12 3 2 12h3v8z" /></Svg>
export const IconExplore = (p: P) => <Svg {...p}><path d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm0 18a8 8 0 1 1 0-16 8 8 0 0 1 0 16zm-5.5-2.5 7.51-3.49L17.5 6.5 9.99 9.99 6.5 17.5zm5.5-6.6a1.1 1.1 0 1 1 0 2.2 1.1 1.1 0 0 1 0-2.2z" /></Svg>
export const IconGrid = (p: P) => <Svg {...p}><path d="M3 3h8v8H3zm10 0h8v8h-8zM3 13h8v8H3zm10 0h8v8h-8z" /></Svg>
export const IconBookmark = (p: P) => <Svg {...p}><path d="M17 3H7a2 2 0 0 0-2 2v16l7-3 7 3V5a2 2 0 0 0-2-2z" /></Svg>
export const IconBookmarkOutline = (p: P) => <Svg {...p}><path d="M17 3H7a2 2 0 0 0-2 2v16l7-3 7 3V5a2 2 0 0 0-2-2zm0 15-5-2.18L7 18V5h10v13z" /></Svg>
export const IconSearch = (p: P) => <Svg {...p}><path d="M15.5 14h-.79l-.28-.27A6.47 6.47 0 0 0 16 9.5 6.5 6.5 0 1 0 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0C7.01 14 5 11.99 5 9.5S7.01 5 9.5 5 14 7.01 14 9.5 11.99 14 9.5 14z" /></Svg>
export const IconPlay = (p: P) => <Svg {...p}><path d="M8 5v14l11-7z" /></Svg>
export const IconReplay = (p: P) => <Svg {...p}><path d="M12 5V1L7 6l5 5V7a6 6 0 1 1-6 6H4a8 8 0 1 0 8-8z" /></Svg>
export const IconMore = (p: P) => <Svg {...p}><path d="M12 8a2 2 0 1 0 0-4 2 2 0 0 0 0 4zm0 2a2 2 0 1 0 0 4 2 2 0 0 0 0-4zm0 6a2 2 0 1 0 0 4 2 2 0 0 0 0-4z" /></Svg>
export const IconCheckCircle = (p: P) => <Svg {...p}><path d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm-2 15-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z" /></Svg>
export const IconCheck = (p: P) => <Svg {...p}><path d="M9 16.2L4.8 12l-1.4 1.4L9 19 21 7l-1.4-1.4z" /></Svg>
export const IconAdd = (p: P) => <Svg {...p}><path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z" /></Svg>
export const IconCopy = (p: P) => <Svg {...p}><path d="M16 1H4a2 2 0 0 0-2 2v14h2V3h12V1zm3 4H8a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2zm0 16H8V7h11v14z" /></Svg>
export const IconDelete = (p: P) => <Svg {...p}><path d="M6 19a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z" /></Svg>
export const IconClose = (p: P) => <Svg {...p}><path d="M19 6.41 17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" /></Svg>
export const IconBack = (p: P) => <Svg {...p}><path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z" /></Svg>
export const IconChevronLeft = (p: P) => <Svg {...p}><path d="M15.41 7.41 14 6l-6 6 6 6 1.41-1.41L10.83 12z" /></Svg>
export const IconChevronRight = (p: P) => <Svg {...p}><path d="M10 6 8.59 7.41 13.17 12l-4.58 4.59L10 18l6-6z" /></Svg>
export const IconExpand = (p: P) => <Svg {...p}><path d="M7 10l5 5 5-5z" /></Svg>
export const IconDownload = (p: P) => <Svg {...p}><path d="M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z" /></Svg>
export const IconFullscreen = (p: P) => <Svg {...p}><path d="M7 14H5v5h5v-2H7v-3zm-2-4h2V7h3V5H5v5zm12 7h-3v2h5v-5h-2v3zM14 5v2h3v3h2V5h-5z" /></Svg>
export const IconLogout = (p: P) => <Svg {...p}><path d="M17 7l-1.41 1.41L18.17 11H8v2h10.17l-2.58 2.58L17 17l5-5zM4 5h8V3H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h8v-2H4V5z" /></Svg>
export const IconPerson = (p: P) => <Svg {...p}><path d="M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z" /></Svg>
export const IconSkipNext = (p: P) => <Svg {...p}><path d="M6 18l8.5-6L6 6v12zM16 6v12h2V6h-2z" /></Svg>
export const IconMenu = (p: P) => <Svg {...p}><path d="M3 18h18v-2H3v2zm0-5h18v-2H3v2zm0-7v2h18V6H3z" /></Svg>
export const IconEdit = (p: P) => <Svg {...p}><path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04a1 1 0 0 0 0-1.41l-2.34-2.34a1 1 0 0 0-1.41 0l-1.83 1.83 3.75 3.75 1.83-1.83z" /></Svg>
export const IconCalendar = (p: P) => <Svg {...p}><path d="M19 4h-1V2h-2v2H8V2H6v2H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V6a2 2 0 0 0-2-2zm0 16H5V10h14v10zm0-12H5V6h14v2zM7 12h5v5H7z" /></Svg>
export const IconHistory = (p: P) => <Svg {...p}><path d="M13 3a9 9 0 0 0-9 9H1l3.89 3.89.07.14L9 12H6a7 7 0 1 1 2.05 4.95l-1.41 1.41A9 9 0 1 0 13 3zm-1 5v5l4.28 2.54.72-1.21-3.5-2.08V8H12z" /></Svg>
export const IconSwap = (p: P) => <Svg {...p}><path d="M6.99 11 3 15l3.99 4v-3H14v-2H6.99v-3zM21 9l-3.99-4v3H10v2h7.01v3L21 9z" /></Svg>
export const IconPause = (p: P) => <Svg {...p}><path d="M6 19h4V5H6v14zm8-14v14h4V5h-4z" /></Svg>
export const IconVolume = (p: P) => <Svg {...p}><path d="M3 9v6h4l5 5V4L7 9H3zm13.5 3A4.5 4.5 0 0 0 14 7.97v8.05c1.48-.73 2.5-2.25 2.5-4.02zM14 3.23v2.06c2.89.86 5 3.54 5 6.71s-2.11 5.85-5 6.71v2.06c4.01-.91 7-4.49 7-8.77s-2.99-7.86-7-8.77z" /></Svg>
export const IconVolumeOff = (p: P) => <Svg {...p}><path d="M16.5 12A4.5 4.5 0 0 0 14 7.97v2.21l2.45 2.45c.03-.2.05-.41.05-.63zm2.5 0c0 .94-.2 1.82-.54 2.64l1.51 1.51A8.8 8.8 0 0 0 21 12c0-4.28-2.99-7.86-7-8.77v2.06c2.89.86 5 3.54 5 6.71zM4.27 3 3 4.27 7.73 9H3v6h4l5 5v-6.73l4.25 4.25c-.67.52-1.42.93-2.25 1.18v2.06a8.99 8.99 0 0 0 3.69-1.81L19.73 21 21 19.73l-9-9L4.27 3zM12 4 9.91 6.09 12 8.18V4z" /></Svg>
export const IconSettings = (p: P) => <Svg {...p}><path d="M19.14 12.94c.04-.3.06-.61.06-.94 0-.32-.02-.64-.07-.94l2.03-1.58a.49.49 0 0 0 .12-.61l-1.92-3.32a.488.488 0 0 0-.59-.22l-2.39.96c-.5-.38-1.03-.7-1.62-.94l-.36-2.54a.484.484 0 0 0-.48-.41h-3.84c-.24 0-.43.17-.47.41l-.36 2.54c-.59.24-1.13.57-1.62.94l-2.39-.96c-.22-.08-.47 0-.59.22L2.74 8.87c-.12.21-.08.47.12.61l2.03 1.58c-.05.3-.09.63-.09.94s.02.64.07.94l-2.03 1.58a.49.49 0 0 0-.12.61l1.92 3.32c.12.22.37.29.59.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.05.24.24.41.48.41h3.84c.24 0 .44-.17.47-.41l.36-2.54c.59-.24 1.13-.56 1.62-.94l2.39.96c.22.08.47 0 .59-.22l1.92-3.32c.12-.22.07-.47-.12-.61l-2.01-1.58zM12 15.6A3.6 3.6 0 1 1 12 8.4a3.6 3.6 0 0 1 0 7.2z" /></Svg>
export const IconSubtitles = (p: P) => <Svg {...p}><path d="M20 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V6a2 2 0 0 0-2-2zM4 12h4v2H4v-2zm10 6H4v-2h10v2zm6 0h-4v-2h4v2zm0-4H10v-2h10v2z" /></Svg>
export const IconReplay10 = (p: P) => <Svg {...p}><path d="M12 5V1L7 6l5 5V7a6 6 0 1 1-6 6H4a8 8 0 1 0 8-8z" /><text x="12" y="15.5" textAnchor="middle" fontSize="7" fontWeight="700" fill="currentColor" fontFamily="system-ui, sans-serif">10</text></Svg>
export const IconForward10 = (p: P) => <Svg {...p}><path d="M12 5V1l5 5-5 5V7a6 6 0 1 0 6 6h2a8 8 0 1 1-8-8z" /><text x="12" y="15.5" textAnchor="middle" fontSize="7" fontWeight="700" fill="currentColor" fontFamily="system-ui, sans-serif">10</text></Svg>

/** Roxy's witch hat: blue cone, gold band, a sparkle at the tip. */
/**
 * The Roxy mark, the phone app's WitchHat (ui/phone/WitchHat.kt) in SVG: a wide brim
 * shaded blue to navy, a drooping cone from light blue to navy with a fold line, a gold
 * band with a buckle and a sparkle at the tip. The gradient ids come from useId so that a
 * hat inside a hidden element (the desktop bar on a phone, the phone header on a
 * desktop) never shadows the ids of a visible one; a shadowed gradient paints nothing.
 */
export const WitchHat = ({ size = 28 }: { size?: number }) => {
  const id = useId().replace(/:/g, '')
  const brim = `hat-brim-${id}`
  const cone = `hat-cone-${id}`
  return (
    <svg width={size} height={size} viewBox="0 0 100 100" aria-hidden="true">
      <defs>
        <linearGradient id={brim} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#3EA6FF" />
          <stop offset="1" stopColor="#1B5FA8" />
        </linearGradient>
        <linearGradient id={cone} x1="0.3" y1="0.2" x2="0.8" y2="0.8">
          <stop offset="0" stopColor="#8FD0FF" />
          <stop offset="0.5" stopColor="#3EA6FF" />
          <stop offset="1" stopColor="#1B5FA8" />
        </linearGradient>
      </defs>
      <ellipse cx="50" cy="79" rx="48" ry="13" fill={`url(#${brim})`} />
      <ellipse cx="50" cy="73" rx="44" ry="6" fill="#8FD0FF" opacity="0.35" />
      <path d="M24 74 C33 44, 44 22, 60 2 C70 10, 72 20, 65 30 C70 48, 73 62, 76 74 Z" fill={`url(#${cone})`} />
      <path d="M50 26 C52 42, 50 56, 46 70" fill="none" stroke="#1B5FA8" strokeOpacity="0.45" strokeWidth="2.5" />
      <path d="M27 64 C44 72, 58 72, 74 64 L76 74 C58 82, 42 82, 24 74 Z" fill="#F2C35B" />
      <rect x="47" y="66.5" width="8" height="10" fill="#1B5FA8" />
      <rect x="49" y="68.5" width="4" height="6" fill="#F2C35B" />
      <path d="M65 8 h14 M72 1 v14" stroke="#FFFFFF" strokeWidth="2" />
    </svg>
  )
}

// ---- music ----
export const IconSkipPrevious = (p: P) => <Svg {...p}><path d="M6 6h2v12H6zm3.5 6 8.5 6V6z" /></Svg>
export const IconShuffle = (p: P) => <Svg {...p}><path d="M10.59 9.17 5.41 4 4 5.41l5.17 5.17 1.42-1.41zM14.5 4l2.04 2.04L4 18.59 5.41 20 17.96 7.46 20 9.5V4h-5.5zm.33 9.41-1.41 1.41 3.13 3.13L14.5 20H20v-5.5l-2.04 2.04-3.13-3.13z" /></Svg>
export const IconRepeat = (p: P) => <Svg {...p}><path d="M7 7h10v3l4-4-4-4v3H5v6h2V7zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2v4z" /></Svg>
export const IconRepeatOne = (p: P) => <Svg {...p}><path d="M7 7h10v3l4-4-4-4v3H5v6h2V7zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2v4zm-4-2V9h-1l-2 1v1h1.5v4H13z" /></Svg>
export const IconHeart = (p: P) => <Svg {...p}><path d="m12 21.35-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z" /></Svg>
export const IconHeartOutline = (p: P) => <Svg {...p}><path d="M16.5 3c-1.74 0-3.41.81-4.5 2.09C10.91 3.81 9.24 3 7.5 3 4.42 3 2 5.42 2 8.5c0 3.78 3.4 6.86 8.55 11.54L12 21.35l1.45-1.32C18.6 15.36 22 12.28 22 8.5 22 5.42 19.58 3 16.5 3zm-4.4 15.55-.1.1-.1-.1C7.14 14.24 4 11.39 4 8.5 4 6.5 5.5 5 7.5 5c1.54 0 3.04.99 3.57 2.36h1.87C13.46 5.99 14.96 5 16.5 5c2 0 3.5 1.5 3.5 3.5 0 2.89-3.14 5.74-7.9 10.05z" /></Svg>
export const IconQueue = (p: P) => <Svg {...p}><path d="M15 6H3v2h12V6zm0 4H3v2h12v-2zM3 16h8v-2H3v2zM17 6v8.18c-.31-.11-.65-.18-1-.18-1.66 0-3 1.34-3 3s1.34 3 3 3 3-1.34 3-3V8h3V6h-5z" /></Svg>
export const IconMic = (p: P) => <Svg {...p}><path d="M12 14c1.66 0 2.99-1.34 2.99-3L15 5c0-1.66-1.34-3-3-3S9 3.34 9 5v6c0 1.66 1.34 3 3 3zm5.3-3c0 3-2.54 5.1-5.3 5.1S6.7 14 6.7 11H5c0 3.41 2.72 6.23 6 6.72V21h2v-3.28c3.28-.48 6-3.3 6-6.72h-1.7z" /></Svg>
export const IconMusicNote = (p: P) => <Svg {...p}><path d="M12 3v10.55c-.59-.34-1.27-.55-2-.55-2.21 0-4 1.79-4 4s1.79 4 4 4 4-1.79 4-4V7h4V3h-6z" /></Svg>
export const IconAlbum = (p: P) => <Svg {...p}><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 14.5c-2.49 0-4.5-2.01-4.5-4.5S9.51 7.5 12 7.5s4.5 2.01 4.5 4.5-2.01 4.5-4.5 4.5zm0-5.5c-.55 0-1 .45-1 1s.45 1 1 1 1-.45 1-1-.45-1-1-1z" /></Svg>
export const IconDrag = (p: P) => <Svg {...p}><path d="M11 18c0 1.1-.9 2-2 2s-2-.9-2-2 .9-2 2-2 2 .9 2 2zm-2-8c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2zm0-6c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2zm6 4c1.1 0 2-.9 2-2s-.9-2-2-2-2 .9-2 2 .9 2 2 2zm0 2c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2zm0 6c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2z" /></Svg>
export const IconPlaylistAdd = (p: P) => <Svg {...p}><path d="M14 10H3v2h11v-2zm0-4H3v2h11V6zm4 8v-4h-2v4h-4v2h4v4h2v-4h4v-2h-4zM3 16h7v-2H3v2z" /></Svg>
export const IconPlaylist = (p: P) => <Svg {...p}><path d="M3 10h11v2H3zm0-4h11v2H3zm0 8h7v2H3zm13-1v8l6-4z" /></Svg>
export const IconTune = (p: P) => <Svg {...p}><path d="M3 17v2h6v-2H3zM3 5v2h10V5H3zm10 16v-2h8v-2h-8v-2h-2v6h2zM7 9v2H3v2h4v2h2V9H7zm14 4v-2H11v2h10zm-6-4h2V7h4V5h-4V3h-2v6z" /></Svg>
export const IconSpectrum = (p: P) => <Svg {...p}><path d="M7 18h2V6H7v12zm4 4h2V2h-2v20zm-8-8h2v-4H3v4zm12 4h2V6h-2v12zm4-8v4h2v-4h-2z" /></Svg>
