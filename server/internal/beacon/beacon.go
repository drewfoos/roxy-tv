// Package beacon answers "who is roxy?" on the LAN so clients can list servers on the
// login screen instead of typing an address (Jellyfin's UDP 7359 and Plex's GDM do the
// same). It is one UDP socket: a probe in, a JSON Info out. Nothing in it needs a
// login, and nothing secret is in the reply.
package beacon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
)

// Probe is the datagram a client broadcasts; anything else is ignored.
const Probe = "who is roxy?"

// Info is the reply. Clients build the server URL from the reply's source address and
// Port unless Advertised is set, in which case Address is what the admin configured
// (ROXY_ADVERTISE_URL, for Docker bridge networks and reverse proxies).
type Info struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Product    string `json:"product"` // always "roxy"
	Version    string `json:"version"`
	Port       int    `json:"port"` // the HTTP port
	Address    string `json:"address,omitempty"`
	Advertised bool   `json:"advertised"`
}

// Run answers probes on udpPort until ctx ends. A bind failure is logged, not fatal:
// the server works without discovery, and the clients' HTTP sweep still finds it.
func Run(ctx context.Context, udpPort int, info func(ctx context.Context) Info) {
	pc, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", udpPort))
	if err != nil {
		slog.Warn("discovery beacon off: cannot listen", "udp", udpPort, "err", err)
		return
	}
	slog.Info("discovery beacon listening", "udp", udpPort)
	go func() { <-ctx.Done(); pc.Close() }()
	buf := make([]byte, 512)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Debug("beacon read", "err", err)
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(string(buf[:n])), Probe) {
			continue
		}
		i := info(ctx)
		i.Product = "roxy"
		if i.Address == "" {
			if ip := localIPFor(from); ip != "" {
				i.Address = "http://" + net.JoinHostPort(ip, strconv.Itoa(i.Port))
			}
		}
		b, err := json.Marshal(i)
		if err != nil {
			continue
		}
		if _, err := pc.WriteTo(b, from); err != nil {
			slog.Debug("beacon reply", "to", from.String(), "err", err)
		}
	}
}

// localIPFor is the address of the interface that faces peer: the OS picks the route,
// and the socket's local end is what the peer can reach.
func localIPFor(peer net.Addr) string {
	c, err := net.Dial("udp4", peer.String())
	if err != nil {
		return ""
	}
	defer c.Close()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return a.IP.String()
	}
	return ""
}

// HTTPPort is the port in a listen address like ":8096" or "0.0.0.0:8096"; 8096 if unparsable.
func HTTPPort(listen string) int {
	_, p, err := net.SplitHostPort(listen)
	if err != nil {
		return 8096
	}
	n, err := strconv.Atoi(p)
	if err != nil || n <= 0 {
		return 8096
	}
	return n
}
