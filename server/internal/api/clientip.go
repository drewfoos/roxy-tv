package api

import (
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

// trustedRealIP replaces chi's RealIP: forwarding headers count only from a private peer.
func trustedRealIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer := peerIP(r)
		if isLocalIP(peer) {
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

// fromHomeNetwork reports whether a request comes from the local network: the connection
// and every address in the forwarding headers must be private. A request relayed from
// the internet by a proxy names its public address in the chain and fails.
func fromHomeNetwork(r *http.Request) bool {
	if !isLocalIP(peerIP(r)) {
		return false
	}
	for _, a := range forwardedChain(r) {
		if !isLocalIP(a) {
			return false
		}
	}
	return true
}
