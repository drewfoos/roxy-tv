package api

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A browser that reached Roxy encrypted is told to stay encrypted (HSTS); one that came
// in the clear is not (the header means nothing there, and the home network's plain
// address has to keep working). "Encrypted" is TLS to the server, or the word of a
// reverse proxy the server was told about: the header alone is anybody's to write.
func TestHSTSOnlyWhenEncrypted(t *testing.T) {
	_, proxies, _ := net.ParseCIDR("10.9.0.0/24")
	h := trustedRealIP([]*net.IPNet{proxies})(securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inClearFromOutside(r) {
			w.Header().Set("X-Test-Clear-Outside", "1")
		}
	})))
	for _, c := range []struct {
		name          string
		peer          string
		tls           bool
		proto, fwdFor string
		hsts, outside bool
	}{
		{name: "the home network, plain", peer: "192.168.1.20:5000"},
		{name: "TLS to the server itself", peer: "203.0.113.5:5000", tls: true, hsts: true},
		{name: "through the configured proxy, https", peer: "10.9.0.2:5000", proto: "https", fwdFor: "203.0.113.5", hsts: true},
		{name: "through the configured proxy, http", peer: "10.9.0.2:5000", proto: "http", fwdFor: "203.0.113.5", outside: true},
		{name: "somebody on the home network writes the header", peer: "192.168.1.66:5000", proto: "https"},
		{name: "the internet, plain", peer: "203.0.113.5:5000", outside: true},
		{name: "the internet, plain, writing the header", peer: "203.0.113.5:5000", proto: "https", outside: true},
		{name: "a VPN address, plain", peer: "100.64.1.2:5000"},
	} {
		r := httptest.NewRequest("GET", "/v1/system/info", nil)
		r.RemoteAddr = c.peer
		if c.tls {
			r.TLS = &tls.ConnectionState{}
		}
		if c.proto != "" {
			r.Header.Set("X-Forwarded-Proto", c.proto)
		}
		if c.fwdFor != "" {
			r.Header.Set("X-Forwarded-For", c.fwdFor)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if got := rec.Header().Get("Strict-Transport-Security") != ""; got != c.hsts {
			t.Errorf("%s: HSTS sent = %v, want %v", c.name, got, c.hsts)
		}
		if got := rec.Header().Get("X-Test-Clear-Outside") != ""; got != c.outside {
			t.Errorf("%s: in the clear from outside = %v, want %v", c.name, got, c.outside)
		}
	}
}
