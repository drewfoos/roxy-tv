package tv.roxy.tv.data

import android.content.Context
import android.net.wifi.WifiManager
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.ProducerScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.channelFlow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withPermit
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import okhttp3.OkHttpClient
import okhttp3.Request
import timber.log.Timber
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.Inet4Address
import java.net.InetAddress
import java.net.NetworkInterface
import java.net.SocketTimeoutException
import java.util.concurrent.TimeUnit

/**
 * A Roxy server found on the network: what the login screen lists. [source] is the IP the
 * answer actually came from. [label] is what to show: the name, or the name with a short
 * id / address when two answers share a name (see [ServerFinder.label]). [ambiguous] marks
 * such a clash (the TV then never picks one by itself); [warning] says why to be careful.
 */
data class FoundServer(
    val id: String, val name: String, val url: String, val version: String,
    val source: String = "",
    val label: String = name,
    val ambiguous: Boolean = false,
    val warning: String? = null,
)

/**
 * Finds Roxy servers on the local network, two ways at once:
 *
 * 1. a UDP broadcast of "who is roxy?" that the server's beacon answers (instant, but
 *    broadcasts do not cross Docker's bridge network or some Wi-Fi isolation), and
 * 2. an HTTP sweep of every address in the phone's subnet on 8096 and 8097, reading
 *    `/v1/system/info` and keeping the ones that say they are Roxy (slower, a second or
 *    two for a /24, but it works wherever the server itself is reachable).
 *
 * The same server reached both ways is reported once. Everything is on a background
 * dispatcher; the flow ends when the window is over.
 */
object ServerFinder {
    const val PORT = 8096
    /** Ports the sweep tries: Roxy's default, then 8097 for a Roxy that sits beside Jellyfin (which keeps 8096). */
    private val SWEEP_PORTS = listOf(PORT, 8097)
    private const val PROBE = "who is roxy?"

    @Serializable
    private data class Reply(
        val id: String = "", val name: String = "", val product: String = "", val version: String = "",
        val port: Int = PORT, val address: String = "", val advertised: Boolean = false,
    )

    private val json = Json { ignoreUnknownKeys = true }

    // Short timeouts: a silent address is the common case, and 254 of them must not take long.
    private val sweepHttp = OkHttpClient.Builder()
        .connectTimeout(350, TimeUnit.MILLISECONDS).readTimeout(700, TimeUnit.MILLISECONDS)
        .build()

    fun find(context: Context, windowMs: Long = 3000): Flow<FoundServer> = channelFlow {
        val seen = HashSet<String>()
        val lock = Any()
        val emit: suspend (FoundServer) -> Unit = { s ->
            // One row per server: by its id (the same server reached by broadcast and by the
            // sweep, maybe under its advertised name) from the same machine. An id is public,
            // so the same id answering from another machine is NOT folded into the first.
            val key = if (s.id.isNotBlank()) "id:${s.id}@${s.source}" else "url:${s.url.trimEnd('/').lowercase()}"
            val fresh = synchronized(lock) { seen.add(key) }
            if (fresh) send(s)
        }
        // Some Wi-Fi drivers drop broadcast replies unless a multicast lock is held.
        val wifi = context.applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
        val lockMc = runCatching { wifi?.createMulticastLock("roxy-find")?.apply { setReferenceCounted(false); acquire() } }.getOrNull()
        try {
            val a = launch { broadcast(windowMs, emit) }
            val b = launch { sweep(emit) }
            a.join(); b.join()
        } finally {
            runCatching { lockMc?.release() }
        }
    }.flowOn(Dispatchers.IO)

    // ---- 1. UDP broadcast ----

    private suspend fun ProducerScope<FoundServer>.broadcast(windowMs: Long, emit: suspend (FoundServer) -> Unit) {
        val targets = buildList {
            add(InetAddress.getByName("255.255.255.255"))
            interfaces().forEach { (_, bcast) -> if (bcast != null) add(bcast) }
        }.distinct()
        val socket = try { DatagramSocket().apply { broadcast = true; soTimeout = 300 } } catch (t: Throwable) { Timber.w(t, "discovery socket"); return }
        socket.use {
            val sender = launch {
                // Three probes over the window: one lost datagram must not mean "no servers".
                repeat(3) { n ->
                    targets.forEach { t -> runCatching { socket.send(DatagramPacket(PROBE.toByteArray(), PROBE.length, t, PORT)) } }
                    delay(windowMs / 3)
                }
            }
            val buf = ByteArray(2048)
            val end = System.currentTimeMillis() + windowMs
            while (System.currentTimeMillis() < end) {
                val p = DatagramPacket(buf, buf.size)
                try { socket.receive(p) } catch (_: SocketTimeoutException) { continue } catch (t: Throwable) { break }
                val text = String(p.data, 0, p.length)
                val r = runCatching { json.decodeFromString(Reply.serializer(), text) }.getOrNull() ?: continue
                if (r.product != "roxy") continue
                // The reply's source address is what reached us; the server's own idea of its
                // address only wins when the admin set one on purpose AND it points at that same
                // machine, a private/CGNAT/link-local address or a Tailscale name. Anything else
                // (a public host) could send the login screen anywhere: use the source instead.
                val src = p.address.hostAddress ?: continue
                val port = r.port.takeIf { it in 1..65535 } ?: PORT
                val fallback = "http://${if (src.contains(':')) "[$src]" else src}:$port"
                val url = if (r.advertised && r.address.isNotBlank()) acceptAdvertised(r.address, src) ?: fallback else fallback
                emit(FoundServer(r.id, r.name.ifBlank { "Roxy Server" }, url, r.version, source = src))
            }
            sender.cancel()
        }
    }

    /** [advertised] if it is a plain http(s) URL on [source] itself or a private / tailnet host; else null. */
    internal fun acceptAdvertised(advertised: String, source: String): String? {
        val a = advertised.trim().trimEnd('/')
        val uri = runCatching { java.net.URI(a) }.getOrNull() ?: return null
        val scheme = uri.scheme?.lowercase()
        if (scheme != "http" && scheme != "https") return null
        if (uri.rawUserInfo != null) return null
        val host = ServerAddress.hostOf(a) ?: return null
        val src = source.lowercase().removePrefix("/").substringBefore('%')
        return if (host == src || ServerAddress.isPrivateOrTailnet(host)) a else null
    }

    /**
     * Labels for a list of found servers: a name shared by answers with different ids (or the
     * same id from different machines) gets a short id or the address after it, and those rows
     * are marked [FoundServer.ambiguous]. [conflict] says whether an answer contradicts a server
     * signed in to before (Prefs.pinConflict).
     */
    fun label(servers: List<FoundServer>, conflict: (FoundServer) -> Boolean): List<FoundServer> {
        val byName = servers.groupBy { it.name.trim().lowercase() }
        return servers.map { s ->
            val same = byName[s.name.trim().lowercase()].orEmpty()
            val clash = same.size > 1
            val idsDiffer = same.map { it.id }.distinct().size > 1 && s.id.isNotBlank()
            val host = s.url.removePrefix("http://").removePrefix("https://")
            val label = when {
                !clash -> s.name
                idsDiffer -> "${s.name}  ·  ${s.id.take(6)}"
                else -> "${s.name}  ·  $host"
            }
            s.copy(
                label = label,
                ambiguous = clash,
                warning = when {
                    conflict(s) -> "This isn't the server you signed in to before"
                    clash -> "More than one server answered with this name"
                    else -> null
                },
            )
        }
    }

    // ---- 2. HTTP sweep of the subnet ----

    private suspend fun ProducerScope<FoundServer>.sweep(emit: suspend (FoundServer) -> Unit) {
        val hosts = interfaces().flatMap { (addr, _) -> hostsAround(addr) }.distinct()
        if (hosts.isEmpty()) return
        val gate = Semaphore(48)
        hosts.map { ip ->
            launch {
                gate.withPermit {
                    for (port in SWEEP_PORTS) {
                        val r = probeHttp("http://$ip:$port") ?: continue
                        emit(FoundServer(r.id, r.name.ifBlank { "Roxy Server" }, "http://$ip:$port", r.version, source = ip))
                        break
                    }
                }
            }
        }.forEach { it.join() }
    }

    private suspend fun probeHttp(base: String): Reply? = withTimeoutOrNull(1500) {
        runCatching {
            sweepHttp.newCall(Request.Builder().url("$base/v1/system/info").get().build()).execute().use { resp ->
                if (!resp.isSuccessful) return@use null
                val r = json.decodeFromString(Reply.serializer(), resp.body.string())
                r.takeIf { it.product == "roxy" }
            }
        }.getOrNull()
    }

    /** Every usable host in the interface's subnet, for prefixes between /22 and /30 (a /16 is not a home). */
    private fun hostsAround(addr: Pair<Inet4Address, Short>): List<String> {
        val (ip, prefix) = addr
        if (prefix < 22 || prefix > 30) return emptyList()
        val bytes = ip.address
        val me = ((bytes[0].toInt() and 255) shl 24) or ((bytes[1].toInt() and 255) shl 16) or ((bytes[2].toInt() and 255) shl 8) or (bytes[3].toInt() and 255)
        val mask = (-1 shl (32 - prefix))
        val net = me and mask
        val first = net + 1
        val last = (net or mask.inv()) - 1
        return (first..last).filter { it != me }.map { "${it ushr 24 and 255}.${it ushr 16 and 255}.${it ushr 8 and 255}.${it and 255}" }
    }

    /** IPv4 addresses (with prefix) and broadcast addresses of the LAN interfaces: Wi-Fi and Ethernet, not loopback or a mobile link. */
    private fun interfaces(): List<Pair<Pair<Inet4Address, Short>, InetAddress?>> = runCatching {
        NetworkInterface.getNetworkInterfaces().toList()
            .filter { it.isUp && !it.isLoopback && !it.isPointToPoint && !it.isVirtual }
            .flatMap { nif -> nif.interfaceAddresses.mapNotNull { ia -> (ia.address as? Inet4Address)?.let { (it to ia.networkPrefixLength) to ia.broadcast } } }
            .filter { (a, _) -> !a.first.isLinkLocalAddress }
    }.getOrElse { Timber.w(it, "interfaces"); emptyList() }
}
