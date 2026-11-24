// How this page was reached, as far as a page can tell.
//
// A password typed here travels the way the page did. Over https it is encrypted; on
// the home network or a VPN (Tailscale) the path is the owner's own. Plain http at any
// other address is neither: everything sent, the password first of all, can be read by
// whoever sits on the way. The apps refuse to send a login there; the web page cannot
// know as much (a name may be anything), so it says so and leaves the decision.

/** Whether [host] names this machine, the home network or a VPN. */
export function privateHost(host: string): boolean {
  const h = host.toLowerCase().replace(/^\[|\]$/g, '')
  if (h === 'localhost' || h.endsWith('.localhost')) return true
  // Names that only resolve at home, and Tailscale's (the VPN encrypts the path itself).
  if (h.endsWith('.local') || h.endsWith('.lan') || h.endsWith('.home.arpa') || h.endsWith('.internal') || h.endsWith('.ts.net')) return true
  const v4 = h.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/)
  if (v4) {
    const [a, b] = [Number(v4[1]), Number(v4[2])]
    return a === 10 || a === 127 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168) || (a === 169 && b === 254) || (a === 100 && b >= 64 && b <= 127)
  }
  if (h.includes(':')) {
    // IPv6: this machine, link-local, and the private range (fc00::/7).
    return h === '::1' || /^fe[89ab]/.test(h) || /^f[cd]/.test(h)
  }
  // One word, no dots: a machine's own name on the home network.
  return !h.includes('.')
}

/** The page came by plain http from an address that is not the owner's own network. */
export function unencryptedOutside(loc: { protocol: string; hostname: string } = window.location): boolean {
  return loc.protocol === 'http:' && !privateHost(loc.hostname)
}

export const UNENCRYPTED_WARNING =
  'This connection is not encrypted. What you type here, your password included, can be read on the way to the server. Reach Roxy through HTTPS or over a VPN such as Tailscale.'
