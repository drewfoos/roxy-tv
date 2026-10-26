// Inline Material Symbols (24 px grid) so the app needs no font or icon CDN: it must
// work on a server with no internet. Paths are Material Design icons (Apache 2.0).
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

/** Roxy's witch hat: blue cone, gold band, a sparkle at the tip. */
export const WitchHat = ({ size = 28 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 64 64" aria-hidden="true">
    <defs>
      <linearGradient id="roxy-hat-g" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stopColor="#8FD0FF" />
        <stop offset="1" stopColor="#1B5FA8" />
      </linearGradient>
    </defs>
    <ellipse cx="32" cy="50" rx="26" ry="7" fill="#1B5FA8" />
    <path d="M20 46 C24 30, 30 20, 40 8 C38 24, 42 36, 46 46 Z" fill="url(#roxy-hat-g)" />
    <path d="M21 42 C28 44, 38 44, 45 42 L46 46 C40 48, 26 48, 20 46 Z" fill="#E7B23A" />
    <path d="M44 4 l1.5 3 3 1.5 -3 1.5 -1.5 3 -1.5 -3 -3 -1.5 3 -1.5 Z" fill="#FFFFFF" />
  </svg>
)
