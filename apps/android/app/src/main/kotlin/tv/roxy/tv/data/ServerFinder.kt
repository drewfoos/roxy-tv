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

/** A Roxy server found on the network: what the login screen lists. */
data class FoundServer(val id: String, val name: String, val url: String, val version: String)

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
            val fresh = synchronized(lock) { seen.add(s.url.trimEnd('/')) }
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
                // address only wins when the admin set one on purpose.
                val url = if (r.advertised && r.address.isNotBlank()) r.address.trimEnd('/') else "http://${p.address.hostAddress}:${r.port}"
                emit(FoundServer(r.id, r.name.ifBlank { "Roxy Server" }, url, r.version))
            }
            sender.cancel()
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
                        emit(FoundServer(r.id, r.name.ifBlank { "Roxy Server" }, "http://$ip:$port", r.version))
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
