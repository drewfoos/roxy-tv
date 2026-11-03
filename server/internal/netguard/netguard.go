// Package netguard keeps server-side fetches of outside images (TMDB, AniList, fanart.tv,
// an admin-picked https URL) from being bounced onto the home network: a redirect may
// only go to https on a public address.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// ErrRefused: a redirect to plain http or to a private, loopback or link-local address.
var ErrRefused = errors.New("refused redirect")

var cgnat = netip.MustParsePrefix("100.64.0.0/10") // Tailscale and carrier NAT

// PublicIP reports whether ip is an ordinary internet address.
func PublicIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	return a.IsValid() && !a.IsLoopback() && !a.IsPrivate() && !a.IsLinkLocalUnicast() &&
		!a.IsLinkLocalMulticast() && !a.IsInterfaceLocalMulticast() && !a.IsMulticast() &&
		!a.IsUnspecified() && !cgnat.Contains(a)
}

// lookup is swapped in tests.
var lookup = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// CheckRedirect is an http.Client CheckRedirect: at most 5 hops, https only, and every
// address the target resolves to must be public.
func CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return fmt.Errorf("%w: too many redirects", ErrRefused)
	}
	if req.URL.Scheme != "https" {
		return fmt.Errorf("%w: %s is not https", ErrRefused, req.URL.Redacted())
	}
	host := req.URL.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if !PublicIP(ip) {
			return fmt.Errorf("%w: %s is not a public address", ErrRefused, host)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	addrs, err := lookup(ctx, host)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRefused, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%w: %s does not resolve", ErrRefused, host)
	}
	for _, a := range addrs {
		if !PublicIP(a.IP) {
			return fmt.Errorf("%w: %s resolves to %s", ErrRefused, host, a.IP)
		}
	}
	return nil
}
