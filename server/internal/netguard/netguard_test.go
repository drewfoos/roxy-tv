package netguard

import (
	"context"
	"net"
	"net/http"
	"testing"
)

func TestCheckRedirect(t *testing.T) {
	lookup = func(_ context.Context, host string) ([]net.IPAddr, error) {
		switch host {
		case "image.tmdb.org":
			return []net.IPAddr{{IP: net.ParseIP("151.101.1.1")}}, nil
		case "evil.example":
			return []net.IPAddr{{IP: net.ParseIP("151.101.1.1")}, {IP: net.ParseIP("192.168.1.50")}}, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host}
	}
	cases := map[string]bool{
		"https://image.tmdb.org/t/p/x.jpg": true,
		"http://image.tmdb.org/t/p/x.jpg":  false,
		"https://evil.example/":            false,
		"https://127.0.0.1:8097/v1/":       false,
		"https://[::1]/":                   false,
		"https://169.254.169.254/latest":   false,
		"https://100.100.1.2/":            false,
		"https://10.0.0.1/":                false,
		"https://8.8.8.8/":                 true,
		"https://nowhere.invalid/":         false,
		"file:///config/roxy.db":           false,
	}
	for u, ok := range cases {
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		err := CheckRedirect(req, []*http.Request{req})
		if (err == nil) != ok {
			t.Errorf("%s: allowed=%v, err=%v", u, err == nil, err)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, "https://8.8.8.8/", nil)
	if CheckRedirect(req, make([]*http.Request, 5)) == nil {
		t.Errorf("a sixth hop must be refused")
	}
}
