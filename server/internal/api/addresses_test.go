package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/config"
)

// The app signed in at home must be able to find the server away from home: the server
// lists every address it is set to answer at, the advertised (home) one first.
func TestAddressesListsTheAdvertisedURLThenTheOthersOnce(t *testing.T) {
	ctx := context.Background()
	s := &Server{cfg: &config.Config{
		AdvertiseURL: "http://192.168.1.50:8097",
		OtherURLs:    []string{"http://100.64.0.7:8097", "http://192.168.1.50:8097", "http://box.example.ts.net:8097"},
	}}
	want := []string{"http://192.168.1.50:8097", "http://100.64.0.7:8097", "http://box.example.ts.net:8097"}
	if got := s.addresses(ctx); !reflect.DeepEqual(got, want) {
		t.Fatalf("addresses = %v, want %v", got, want)
	}
	if got := (&Server{cfg: &config.Config{}}).addresses(ctx); len(got) != 0 {
		t.Fatalf("nothing set: addresses = %v, want none", got)
	}
}

// What an admin may save: the apps only move a login to https, or to a private /
// Tailscale IP or a .ts.net name over plain http, so nothing else is accepted (it would
// be saved and never used); and only a bare address, written one way.
func TestCleanAddress(t *testing.T) {
	good := map[string]string{
		"http://100.64.0.7:8097":          "http://100.64.0.7:8097",
		"  http://100.64.0.7:8097/ ":      "http://100.64.0.7:8097",
		"http://192.168.1.50:8097":        "http://192.168.1.50:8097",
		"http://10.0.0.5":                 "http://10.0.0.5",
		"http://172.20.1.1:8097":          "http://172.20.1.1:8097",
		"HTTP://Box.Example.TS.net:8097":  "http://box.example.ts.net:8097",
		"https://roxy.example.com":        "https://roxy.example.com",
		"https://Roxy.Example.com:8443/":  "https://roxy.example.com:8443",
		"https://203.0.113.9:8097":        "https://203.0.113.9:8097",
		"http://[fd7a:115c:a1e0::7]:8097": "http://[fd7a:115c:a1e0::7]:8097",
		"https://[2001:db8::1]:8097":      "https://[2001:db8::1]:8097",
	}
	for in, want := range good {
		got, err := cleanAddress(in)
		if err != nil || got != want {
			t.Errorf("cleanAddress(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"100.64.0.7:8097",                  // no scheme
		"ftp://100.64.0.7",                 // not http(s)
		"http://",                          // no host
		"http://roxy.example.com:8097",     // plain http across the internet
		"http://203.0.113.9:8097",          // the same, by IP
		"http://[2001:db8::1]:8097",        // and by IPv6
		"http://myserver:8097",             // a name the apps would not trust over http
		"http://evil.ts.net.example.com",   // not a .ts.net name
		"http://172.32.0.1:8097",           // just outside 172.16/12
		"http://100.128.0.1:8097",          // just outside 100.64/10
		"http://127.0.0.1:8097",            // the server itself
		"https://localhost:8097",           // the same
		"http://0.0.0.0:8097",              // no address at all
		"https://roxy.example.com/roxy",    // a path
		"https://roxy.example.com/?a=1",    // a query
		"https://user:pw@roxy.example.com", // a login in the address
		"https://roxy.example.com/#x",      // a fragment
		"https://" + strings.Repeat("a", 200) + ".example.com",
	}
	for _, in := range bad {
		if got, err := cleanAddress(in); err == nil {
			t.Errorf("cleanAddress(%q) = %q, want it refused", in, got)
		}
	}
}

func TestCleanAddressesDropsBlanksAndRepeatsAndCapsTheCount(t *testing.T) {
	got, err := cleanAddresses([]string{"", "http://100.64.0.7:8097", "  ", "http://100.64.0.7:8097/", "https://roxy.example.com"})
	want := []string{"http://100.64.0.7:8097", "https://roxy.example.com"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v; want %v", got, err, want)
	}
	if got, err := cleanAddresses(nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("an empty list is an empty list (not null in JSON): %#v, %v", got, err)
	}
	var many []string
	for i := 0; i <= maxAddresses; i++ {
		many = append(many, "http://10.0.0."+string(rune('1'+i)))
	}
	if _, err := cleanAddresses(many); err == nil {
		t.Fatalf("%d addresses were accepted, the limit is %d", len(many), maxAddresses)
	}
	if _, err := cleanAddresses([]string{"https://roxy.example.com", "nonsense"}); err == nil {
		t.Fatal("one bad address must refuse the whole list")
	}
}

// The check behind "is this address this server?": only Roxy's own answer with this
// server's id counts. Another Roxy, another service, a redirect and silence are each told
// apart, and no redirect is followed.
func TestProbeAddressTellsThisServerFromEverythingElse(t *testing.T) {
	info := func(body string, status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/system/info" {
				t.Errorf("asked for %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Error("a credential was sent to an address being checked")
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
	}
	same := info(`{"product":"roxy","id":"srv-1"}`, 200)
	defer same.Close()
	other := info(`{"product":"roxy","id":"srv-2"}`, 200)
	defer other.Close()
	jelly := info(`{"ProductName":"Something else","Id":"srv-1"}`, 200)
	defer jelly.Close()
	page := info(`<html>router login</html>`, 200)
	defer page.Close()
	failing := info(`{"product":"roxy","id":"srv-1"}`, 502)
	defer failing.Close()
	followed := false
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			followed = true
			_, _ = w.Write([]byte(`{"product":"roxy","id":"srv-1"}`))
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer redirect.Close()
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	ctx := context.Background()
	for _, c := range []struct{ name, url, want string }{
		{"this server", same.URL, addrOK},
		{"another Roxy", other.URL, addrOther},
		{"another product", jelly.URL, addrNotRoxy},
		{"a web page", page.URL, addrNotRoxy},
		{"a proxy's error", failing.URL, addrNotRoxy},
		{"a redirect", redirect.URL, addrNotRoxy},
		{"nothing there", deadURL, addrUnreachable},
	} {
		if got := probeAddress(ctx, c.url, "srv-1"); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if followed {
		t.Error("the redirect was followed")
	}
	// A server that does not know its own id never calls an answer "this server".
	if got := probeAddress(ctx, same.URL, ""); got == addrOK {
		t.Error("an empty server id matched")
	}
}

// stubProbe answers address checks from a table (anything missing is unreachable) and
// records what was asked.
func stubProbe(t *testing.T, states map[string]string) *[]string {
	t.Helper()
	var asked []string
	var mu = make(chan struct{}, 1)
	old := probeAddress
	probeAddress = func(_ context.Context, address, serverID string) string {
		mu <- struct{}{}
		asked = append(asked, address)
		<-mu
		if serverID == "" {
			t.Error("the check was made without this server's id")
		}
		if s, ok := states[address]; ok {
			return s
		}
		return addrUnreachable
	}
	t.Cleanup(func() { probeAddress = old })
	return &asked
}

type addressReply struct {
	Advertised      string         `json:"advertised"`
	Addresses       []string       `json:"addresses"`
	FromEnvironment bool           `json:"fromEnvironment"`
	Checks          []addressCheck `json:"checks"`
	Error           string         `json:"error"`
}

func (w *authWorld) addresses(t *testing.T, method, tok, body string) (int, addressReply) {
	t.Helper()
	rec := w.do(method, "/v1/settings/addresses", func(r *http.Request) {
		bearer(tok)(r)
		if body != "" {
			r.Body = readCloser(body)
		}
	})
	var out addressReply
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: %v in %q", method, err, rec.Body.String())
		}
	}
	return rec.Code, out
}

func readCloser(s string) *bodyReader { return &bodyReader{strings.NewReader(s)} }

type bodyReader struct{ *strings.Reader }

func (*bodyReader) Close() error { return nil }

// meAddresses is what a signed-in app is told by /v1/auth/me.
func (w *authWorld) meAddresses(t *testing.T) ([]string, string) {
	t.Helper()
	rec := w.do("GET", "/v1/auth/me", bearer(w.aliceTok))
	var me struct {
		Addresses []string `json:"addresses"`
		ServerID  string   `json:"serverId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	return me.Addresses, me.ServerID
}

// The point of the setting: an admin saves the server's other addresses once, and every
// signed-in app gets them (after the advertised one) with the server's id.
func TestSavedAddressesReachEverySignedInApp(t *testing.T) {
	w := newAuthWorld(t)
	w.cfg.AdvertiseURL = "http://192.168.1.50:8097"
	asked := stubProbe(t, map[string]string{"http://100.64.0.7:8097": addrOK})

	if code, got := w.addresses(t, "GET", w.adminTok, ""); code != 200 || got.Addresses == nil || len(got.Addresses) != 0 || got.FromEnvironment || got.Advertised != "http://192.168.1.50:8097" {
		t.Fatalf("before anything is saved: %d %+v", code, got)
	}
	code, got := w.addresses(t, "PUT", w.adminTok, `{"addresses":[" http://100.64.0.7:8097/ ","https://roxy.example.com",""]}`)
	want := []string{"http://100.64.0.7:8097", "https://roxy.example.com"}
	if code != 200 || !reflect.DeepEqual(got.Addresses, want) {
		t.Fatalf("PUT = %d %+v", code, got)
	}
	// Each address was asked, and one that does not answer from the server is saved all
	// the same and reported.
	wantChecks := []addressCheck{{"http://100.64.0.7:8097", addrOK}, {"https://roxy.example.com", addrUnreachable}}
	if !reflect.DeepEqual(got.Checks, wantChecks) || len(*asked) != 2 {
		t.Fatalf("checks = %+v (asked %v), want %+v", got.Checks, *asked, wantChecks)
	}
	if _, again := w.addresses(t, "GET", w.adminTok, ""); !reflect.DeepEqual(again.Addresses, want) {
		t.Fatalf("GET after the save = %+v", again)
	}
	addrs, id := w.meAddresses(t)
	if !reflect.DeepEqual(addrs, append([]string{"http://192.168.1.50:8097"}, want...)) || id == "" {
		t.Fatalf("/v1/auth/me: addresses %v, serverId %q", addrs, id)
	}

	// Saving an empty list takes them away again.
	if code, got := w.addresses(t, "PUT", w.adminTok, `{"addresses":[]}`); code != 200 || len(got.Addresses) != 0 {
		t.Fatalf("clearing: %d %+v", code, got)
	}
	if addrs, _ := w.meAddresses(t); !reflect.DeepEqual(addrs, []string{"http://192.168.1.50:8097"}) {
		t.Fatalf("after clearing, /v1/auth/me lists %v", addrs)
	}
}

// With no advertised URL, the saved addresses alone are listed.
func TestSavedAddressesAreListedWithoutAnAdvertisedURL(t *testing.T) {
	w := newAuthWorld(t)
	stubProbe(t, nil)
	if code, _ := w.addresses(t, "PUT", w.adminTok, `{"addresses":["http://100.64.0.7:8097"]}`); code != 200 {
		t.Fatal(code)
	}
	if addrs, id := w.meAddresses(t); !reflect.DeepEqual(addrs, []string{"http://100.64.0.7:8097"}) || id == "" {
		t.Fatalf("/v1/auth/me: %v %q", addrs, id)
	}
}

// Only an admin reads or changes them; nothing is checked or saved for anyone else.
func TestAddressSettingsAreAdminOnly(t *testing.T) {
	w := newAuthWorld(t)
	asked := stubProbe(t, nil)
	for _, tok := range []string{w.aliceTok, "", w.apiKey} {
		for _, method := range []string{"GET", "PUT"} {
			code, _ := w.addresses(t, method, tok, `{"addresses":["http://100.64.0.7:8097"]}`)
			if code != 401 && code != 403 {
				t.Errorf("%s with token %q = %d, want 401/403", method, tok, code)
			}
		}
	}
	if len(*asked) != 0 {
		t.Errorf("addresses were checked for a non-admin: %v", *asked)
	}
	if addrs, _ := w.meAddresses(t); len(addrs) != 0 {
		t.Errorf("a non-admin's PUT was saved: %v", addrs)
	}
}

// An address where a different Roxy server answers is a mistake (a friend's server, an
// old box): the whole save is refused and what was saved before stays.
func TestAnAddressOfAnotherServerIsRefusedAndNothingChanges(t *testing.T) {
	w := newAuthWorld(t)
	stubProbe(t, map[string]string{"http://100.64.0.7:8097": addrOK, "http://100.64.0.9:8097": addrOther, "http://100.64.0.8:8097": addrNotRoxy})
	if code, _ := w.addresses(t, "PUT", w.adminTok, `{"addresses":["http://100.64.0.7:8097"]}`); code != 200 {
		t.Fatal(code)
	}
	code, got := w.addresses(t, "PUT", w.adminTok, `{"addresses":["https://roxy.example.com","http://100.64.0.9:8097"]}`)
	if code != 400 || !strings.Contains(got.Error, "http://100.64.0.9:8097") || !strings.Contains(got.Error, "different Roxy server") {
		t.Fatalf("PUT = %d %q", code, got.Error)
	}
	if addrs, _ := w.meAddresses(t); !reflect.DeepEqual(addrs, []string{"http://100.64.0.7:8097"}) {
		t.Fatalf("the refused save changed the list: %v", addrs)
	}
	// Something that is not Roxy answering (a router's page when the server cannot reach
	// its own outside address) is not proof of a mistake: saved, and reported.
	code, got = w.addresses(t, "PUT", w.adminTok, `{"addresses":["http://100.64.0.8:8097"]}`)
	if code != 200 || len(got.Checks) != 1 || got.Checks[0].State != addrNotRoxy {
		t.Fatalf("PUT = %d %+v", code, got)
	}
}

// A bad address refuses the save before anything is asked or stored.
func TestABadAddressIsRefusedBeforeAnythingIsAsked(t *testing.T) {
	w := newAuthWorld(t)
	asked := stubProbe(t, nil)
	for _, body := range []string{
		`{"addresses":["http://roxy.example.com:8097"]}`,
		`{"addresses":["https://roxy.example.com","100.64.0.7:8097"]}`,
		`{"addresses":"http://100.64.0.7:8097"}`,
		`not json`,
	} {
		if code, got := w.addresses(t, "PUT", w.adminTok, body); code != 400 || got.Error == "" {
			t.Errorf("PUT %s = %d %+v, want 400", body, code, got)
		}
	}
	if len(*asked) != 0 {
		t.Errorf("asked %v", *asked)
	}
	if addrs, _ := w.meAddresses(t); len(addrs) != 0 {
		t.Errorf("saved %v", addrs)
	}
}

// ROXY_OTHER_URLS, where it is set, stays in charge: the page shows its list as fixed,
// a save is refused, and a list saved earlier is not what the apps get.
func TestTheEnvironmentListWinsAndCannotBeChangedHere(t *testing.T) {
	w := newAuthWorld(t)
	asked := stubProbe(t, nil)
	if code, _ := w.addresses(t, "PUT", w.adminTok, `{"addresses":["https://saved.example.com"]}`); code != 200 {
		t.Fatal(code)
	}
	*asked = nil
	w.cfg.OtherURLs = []string{"http://100.64.0.7:8097"}

	code, got := w.addresses(t, "GET", w.adminTok, "")
	if code != 200 || !got.FromEnvironment || !reflect.DeepEqual(got.Addresses, []string{"http://100.64.0.7:8097"}) {
		t.Fatalf("GET = %d %+v", code, got)
	}
	code, got = w.addresses(t, "PUT", w.adminTok, `{"addresses":["https://roxy.example.com"]}`)
	if code != 409 || !strings.Contains(got.Error, "ROXY_OTHER_URLS") || len(*asked) != 0 {
		t.Fatalf("PUT = %d %q (asked %v)", code, got.Error, *asked)
	}
	if addrs, _ := w.meAddresses(t); !reflect.DeepEqual(addrs, []string{"http://100.64.0.7:8097"}) {
		t.Fatalf("/v1/auth/me lists %v", addrs)
	}
	// Taken out of the environment again, the saved list is back.
	w.cfg.OtherURLs = nil
	if addrs, _ := w.meAddresses(t); !reflect.DeepEqual(addrs, []string{"https://saved.example.com"}) {
		t.Fatalf("without the variable, /v1/auth/me lists %v", addrs)
	}
}
