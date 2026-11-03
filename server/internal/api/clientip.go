package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Client addresses. A reverse proxy (nginx, Caddy, Traefik) in front of Roxy puts the
// real client in X-Forwarded-For / X-Real-IP; anyone else can write those headers too.
// So they are believed only when the connection itself comes from a private address
// (where a proxy would be), and then the last X-Forwarded-For entry (the one the proxy
// added) wins. This keeps per-address rate limits and the sign-in lockout honest.

var privateNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{
		"127.0.0.0/8", "::1/128", // this machine
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", // home / office networks
		"fc00::/7", "fe80::/10", "169.254.0.0/16", // IPv6 private, link-local
		"100.64.0.0/10", // CGNAT range used by Tailscale and similar private networks
	} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

// isLocalIP reports whether ip is on a private network (or this machine).
func isLocalIP(ip string) bool {
	p := net.ParseIP(strings.Trim(ip, "[] "))
	if p == nil {
		return false
	}
	for _, n := range privateNets {
		if n.Contains(p) {
			return true
		}
	}
	return false
}

func peerIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// forwardedChain is every address the forwarding headers name.
func forwardedChain(r *http.Request) []string {
	var out []string
	for _, part := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	if x := strings.TrimSpace(r.Header.Get("X-Real-IP")); x != "" {
		out = append(out, x)
	}
	return out
}

// peerKey carries the connection's own address, before any forwarding header.
type peerKey struct{}

// parseNets reads "10.0.0.5" / "10.0.0.0/8" entries; bad ones are skipped.
func parseNets(list []string) []*net.IPNet {
	var out []*net.IPNet
	for _, v := range list {
		if !strings.Contains(v, "/") {
			if ip := net.ParseIP(v); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				v = fmt.Sprintf("%s/%d", v, bits)
			}
		}
		if _, n, err := net.ParseCIDR(v); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func inNets(ip string, nets []*net.IPNet) bool {
	p := net.ParseIP(strings.Trim(ip, "[] "))
	if p == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(p) {
			return true
		}
	}
	return false
}

// trustedRealIP replaces chi's RealIP. Forwarding headers count only when the connection
// comes from a configured reverse proxy (ROXY_TRUSTED_PROXIES); anyone else could write
// them. (Believing every private peer let any LAN, Tailscale, container or IPv6-via-Docker
// client pick its own address: past the sign-in throttle and the rate limit, and into
// "home network".) The real peer is kept for fromHomeNetwork.
func trustedRealIP(trusted []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer := peerIP(r)
			r = r.WithContext(context.WithValue(r.Context(), peerKey{}, peer))
			if len(trusted) > 0 && inNets(peer, trusted) {
				xff := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
				client := strings.TrimSpace(xff[len(xff)-1])
				if client == "" {
					client = strings.TrimSpace(r.Header.Get("X-Real-IP"))
				}
				if net.ParseIP(client) != nil {
					r.RemoteAddr = net.JoinHostPort(client, "0")
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// connPeer: the connection's own address (before trustedRealIP), else RemoteAddr's.
func connPeer(r *http.Request) string {
	if p, ok := r.Context().Value(peerKey{}).(string); ok {
		return p
	}
	return peerIP(r)
}

// fromHomeNetwork reports whether a request comes from the local network: the connection
// and every address in the forwarding headers must be private. A request relayed from
// the internet by a proxy names its public address in the chain and fails.
func fromHomeNetwork(r *http.Request) bool {
	if !isLocalIP(connPeer(r)) || !isLocalIP(peerIP(r)) {
		return false
	}
	for _, a := range forwardedChain(r) {
		if !isLocalIP(a) {
			return false
		}
	}
	return true
}

// maxBody caps every request body (a 300 MB sign-in body took the server to 1.9 GB and
// wrote a 268 MB log line). Image uploads are the largest legitimate bodies.
const maxBody = 32 << 20

func limitBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		next.ServeHTTP(w, r)
	})
}
