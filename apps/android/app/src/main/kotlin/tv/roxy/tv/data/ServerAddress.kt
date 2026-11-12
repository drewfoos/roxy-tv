package tv.roxy.tv.data

import java.net.Inet6Address
import java.net.InetAddress

/**
 * What kind of address a server URL points at, without any DNS lookup: used to decide
 * whether a discovery answer's advertised address is believable and whether to warn that
 * a plain-http address is not on the home network.
 */
object ServerAddress {

    /** The host of [url] (lower case, no brackets), or null when it has none. Accepts "host:port" without a scheme. */
    fun hostOf(url: String): String? {
        val s = url.trim().let { if (it.contains("://")) it else "http://$it" }
        val host = runCatching { java.net.URI(s).host }.getOrNull() ?: return null
        return host.removePrefix("[").removeSuffix("]").lowercase().trimEnd('.').ifEmpty { null }
    }

    /**
     * An IP literal in a private range (RFC 1918, CGNAT/Tailscale 100.64/10, link-local,
     * loopback, IPv6 ULA / link-local) or a Tailscale MagicDNS name (*.ts.net).
     */
    fun isPrivateOrTailnet(host: String): Boolean {
        val h = host.lowercase().removePrefix("[").removeSuffix("]").trimEnd('.')
        if (h.endsWith(".ts.net")) return true
        ipv4(h)?.let { b ->
            val a = b[0]; val c = b[1]
            return a == 10 || a == 127 ||
                (a == 172 && c in 16..31) ||
                (a == 192 && c == 168) ||
                (a == 100 && c in 64..127) ||
                (a == 169 && c == 254)
        }
        if (h.contains(':')) {
            val addr = runCatching { InetAddress.getByName(h) }.getOrNull() as? Inet6Address ?: return false
            val first = addr.address[0].toInt() and 0xff
            val second = addr.address[1].toInt() and 0xff
            return addr.isLoopbackAddress || addr.isLinkLocalAddress || (first and 0xfe) == 0xfc ||
                (first == 0xfe && (second and 0xc0) == 0x80)
        }
        return false
    }

    /** A Tailscale address: 100.64/10 or a MagicDNS name (*.ts.net). */
    fun isTailnet(host: String): Boolean {
        val h = host.lowercase().removePrefix("[").removeSuffix("]").trimEnd('.')
        if (h.endsWith(".ts.net")) return true
        val b = ipv4(h) ?: return false
        return b[0] == 100 && b[1] in 64..127
    }

    /**
     * How good [url] is as a way to the server when several answer: 0 on the home network
     * (the direct, fast way), 1 over Tailscale (may be relayed), 2 anything else.
     */
    fun rank(url: String): Int {
        val host = hostOf(url) ?: return 2
        return when {
            isTailnet(host) -> 1
            isHomeNetwork(host) -> 0
            else -> 2
        }
    }

    /**
     * Whether the app may move its login to [url] because the server listed it: https, or a
     * private / Tailscale address. Plain http to anywhere else would send the login across
     * the internet in the clear, so such an address is only used if someone typed it.
     */
    fun usableAlternate(url: String): Boolean {
        val t = url.trim()
        if (!t.startsWith("http://", ignoreCase = true) && !t.startsWith("https://", ignoreCase = true)) return false
        val host = hostOf(t) ?: return false
        return t.startsWith("https://", ignoreCase = true) || isPrivateOrTailnet(host)
    }

    /**
     * Private as above, or a name that only means something on a home network: a single
     * label ("myserver") or .local / .lan / .home.arpa / .internal. For the "not encrypted"
     * hint only; never for trusting a discovery answer.
     */
    fun isHomeNetwork(host: String): Boolean {
        val h = host.lowercase().trimEnd('.')
        if (isPrivateOrTailnet(h)) return true
        if (ipv4(h) != null || h.contains(':')) return false
        return !h.contains('.') || listOf(".local", ".lan", ".home.arpa", ".internal", ".attlocal.net").any { h.endsWith(it) }
    }

    /** True when [url] is plain http to a host outside the home network / tailnet: show "Not encrypted". */
    fun cleartextOutsideHome(url: String): Boolean {
        val t = url.trim()
        if (t.isEmpty() || t.startsWith("https://", ignoreCase = true)) return false
        val host = hostOf(t) ?: return false
        return !isHomeNetwork(host)
    }

    private fun ipv4(h: String): IntArray? {
        val parts = h.split('.')
        if (parts.size != 4) return null
        val out = IntArray(4)
        for (i in 0 until 4) {
            val p = parts[i]
            if (p.isEmpty() || p.length > 3 || !p.all { it.isDigit() }) return null
            out[i] = p.toInt().takeIf { it in 0..255 } ?: return null
        }
        return out
    }
}
