package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func req(peer string, headers map[string]string) *http.Request {
	r := httptest.NewRequest("GET", "/v1/setup", nil)
	r.RemoteAddr = peer
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestFromHomeNetwork(t *testing.T) {
	cases := []struct {
		name string
		r    *http.Request
		want bool
	}{
		{"phone on wifi", req("192.168.1.20:5000", nil), true},
		{"same machine", req("127.0.0.1:5000", nil), true},
		{"tailscale", req("100.101.1.2:5000", nil), true},
		{"internet direct", req("203.0.113.9:5000", nil), false},
		{"internet faking a header", req("203.0.113.9:5000", map[string]string{"X-Forwarded-For": "192.168.1.5"}), false},
		{"internet through a local proxy", req("127.0.0.1:5000", map[string]string{"X-Forwarded-For": "203.0.113.9"}), false},
		{"spoof prepended through a proxy", req("127.0.0.1:5000", map[string]string{"X-Forwarded-For": "192.168.1.5, 203.0.113.9"}), false},
		{"lan through a local proxy", req("127.0.0.1:5000", map[string]string{"X-Forwarded-For": "192.168.1.5"}), true},
		{"x-real-ip from the internet", req("127.0.0.1:5000", map[string]string{"X-Real-IP": "8.8.8.8"}), false},
	}
	for _, c := range cases {
		if got := fromHomeNetwork(c.r); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTrustedRealIP(t *testing.T) {
	var seen string
	var home bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen, home = peerIP(r), fromHomeNetwork(r) })
	// No proxy configured: nobody picks their address with a header, a LAN or IPv6-via-
	// Docker client included (it used to rotate addresses past the sign-in throttle).
	h := trustedRealIP(nil)(next)
	h.ServeHTTP(httptest.NewRecorder(), req("203.0.113.9:5000", map[string]string{"X-Forwarded-For": "1.2.3.4"}))
	if seen != "203.0.113.9" {
		t.Errorf("spoofed header believed: %s", seen)
	}
	h.ServeHTTP(httptest.NewRecorder(), req("172.30.0.1:5000", map[string]string{"X-Forwarded-For": "203.0.113.77"}))
	if seen != "172.30.0.1" {
		t.Errorf("LAN peer chose its address: %s", seen)
	}
	// A configured proxy: its (last) entry is the client, and an internet client behind it
	// is not "home".
	h = trustedRealIP(parseNets([]string{"127.0.0.1"}))(next)
	h.ServeHTTP(httptest.NewRecorder(), req("127.0.0.1:5000", map[string]string{"X-Forwarded-For": "1.2.3.4, 198.51.100.7"}))
	if seen != "198.51.100.7" || home {
		t.Errorf("proxy client: got %s home=%v", seen, home)
	}
	h.ServeHTTP(httptest.NewRecorder(), req("127.0.0.1:5000", map[string]string{"X-Forwarded-For": "192.168.1.5"}))
	if seen != "192.168.1.5" || !home {
		t.Errorf("LAN client through proxy: got %s home=%v", seen, home)
	}
}
