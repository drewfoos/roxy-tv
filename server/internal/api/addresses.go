package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

// The server's other addresses (its Tailscale one, a domain): the admin's list, kept in
// settings. ROXY_OTHER_URLS, when set, is used instead and the list cannot be changed here.
const (
	addressesSetting = "server_addresses"
	maxAddresses     = 8
	maxAddressLen    = 200
)

var tailnetRange = netip.MustParsePrefix("100.64.0.0/10")

// addresses lists the URLs this server answers at, as the apps are told: the advertised
// one first, then ROXY_OTHER_URLS or, without it, the admin's saved list. Empty when
// nothing is set.
func (s *Server) addresses(ctx context.Context) []string {
	var out []string
	seen := map[string]bool{}
	for _, u := range append([]string{s.cfg.AdvertiseURL}, s.otherAddresses(ctx)...) {
		if u != "" && !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

// otherAddresses: the environment's list when there is one, else the saved one.
func (s *Server) otherAddresses(ctx context.Context) []string {
	if len(s.cfg.OtherURLs) > 0 {
		return s.cfg.OtherURLs
	}
	var saved []string
	if s.store != nil {
		_, _ = s.store.GetSetting(ctx, addressesSetting, &saved)
	}
	return saved
}

// cleanAddress checks one address an admin typed and returns it as the apps will get it
// (scheme://host[:port], lower case, no trailing slash). The rule for plain http is the
// apps' own: they only move a login to https, or to a private / Tailscale IP or a
// *.ts.net name, so anything else would be saved and never used.
func cleanAddress(raw string) (string, error) {
	t := strings.TrimSpace(raw)
	if len(t) > maxAddressLen {
		return "", errors.New("That address is too long.")
	}
	u, err := url.Parse(t)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("%q is not an address. Write it like http://100.64.0.7:8097 or https://roxy.example.com.", t)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return "", fmt.Errorf("%q: only the address itself, with nothing after the port.", t)
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" {
		return "", fmt.Errorf("%q has no host name.", t)
	}
	ip, isIP := netip.Addr{}, false
	if a, err := netip.ParseAddr(host); err == nil {
		ip, isIP = a.Unmap(), true
	}
	if host == "localhost" || (isIP && (ip.IsLoopback() || ip.IsUnspecified())) {
		return "", fmt.Errorf("%q only means something on the server itself; other devices cannot use it.", t)
	}
	if u.Scheme == "http" {
		private := isIP && (ip.IsPrivate() || ip.IsLinkLocalUnicast() || tailnetRange.Contains(ip))
		if !private && !(!isIP && strings.HasSuffix(host, ".ts.net")) {
			return "", fmt.Errorf("%q: plain http is only for a home-network or Tailscale IP address, or a .ts.net name. The apps do not send a login anywhere else without https.", t)
		}
	}
	out := u.Scheme + "://" + strings.ToLower(u.Host)
	return strings.TrimSuffix(out, "."), nil
}

// cleanAddresses checks a whole list: empty lines dropped, repeats dropped, at most
// maxAddresses.
func cleanAddresses(raw []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, r := range raw {
		if strings.TrimSpace(r) == "" {
			continue
		}
		a, err := cleanAddress(r)
		if err != nil {
			return nil, err
		}
		if seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	if len(out) > maxAddresses {
		return nil, fmt.Errorf("At most %d addresses.", maxAddresses)
	}
	return out, nil
}

// What asking an address "are you this server?" came to.
const (
	addrOK          = "ok"          // this server answered there
	addrOther       = "other"       // a different Roxy server answered
	addrNotRoxy     = "notRoxy"     // something answered, and it is not Roxy
	addrUnreachable = "unreachable" // nothing answered from where the server runs
)

type addressCheck struct {
	Address string `json:"address"`
	State   string `json:"state"`
}

// probeAddress asks address for /v1/system/info, as the apps do before they move a login
// there, and compares the server id. No redirects are followed, nothing of the answer is
// kept, and no credential is sent. Swapped in tests.
var probeAddress = func(ctx context.Context, address, serverID string) string {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/v1/system/info", nil)
	if err != nil {
		return addrUnreachable
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return addrUnreachable
	}
	defer resp.Body.Close()
	var info struct {
		Product string `json:"product"`
		ID      string `json:"id"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&info) != nil || info.Product != "roxy" {
		return addrNotRoxy
	}
	if serverID == "" || info.ID != serverID {
		return addrOther
	}
	return addrOK
}

func (s *Server) checkAddresses(ctx context.Context, list []string) []addressCheck {
	id, _ := s.store.ServerID(ctx)
	out := make([]addressCheck, len(list))
	var wg sync.WaitGroup
	for i, a := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = addressCheck{Address: a, State: probeAddress(ctx, a, id)}
		}()
	}
	wg.Wait()
	return out
}

func (s *Server) addressSettings(ctx context.Context) map[string]any {
	other := s.otherAddresses(ctx)
	if other == nil {
		other = []string{}
	}
	return map[string]any{
		"advertised":      s.cfg.AdvertiseURL,
		"addresses":       other,
		"fromEnvironment": len(s.cfg.OtherURLs) > 0,
	}
}

// GET /v1/settings/addresses -> {advertised, addresses, fromEnvironment} (admin).
// addresses are the other ways to this server the apps are told about; fromEnvironment
// means ROXY_OTHER_URLS sets them and they cannot be changed here.
func (s *Server) addressSettingsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	writeJSON(w, 200, s.addressSettings(r.Context()))
}

// PUT /v1/settings/addresses {addresses: [url]} -> the same as GET plus
// checks: [{address, state}] (admin). Each address is asked whether it is this server:
// one where a different Roxy server answers is refused; one that does not answer from
// where the server runs is saved all the same (the apps ask again before using it).
func (s *Server) addressSettingsPut(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if len(s.cfg.OtherURLs) > 0 {
		writeError(w, 409, "These addresses are set by ROXY_OTHER_URLS where the server is started. Change them there, or remove it to set them here.")
		return
	}
	var body struct {
		Addresses []string `json:"addresses"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeError(w, 400, "bad body")
		return
	}
	list, err := cleanAddresses(body.Addresses)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	checks := s.checkAddresses(r.Context(), list)
	for _, c := range checks {
		if c.State == addrOther {
			writeError(w, 400, c.Address+" is a different Roxy server, not this one. Nothing was saved.")
			return
		}
	}
	if err := s.store.SetSetting(r.Context(), addressesSetting, list); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	out := s.addressSettings(r.Context())
	out["checks"] = checks
	writeJSON(w, 200, out)
}
