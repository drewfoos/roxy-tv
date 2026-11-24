package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/library"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"github.com/roxy-media/roxy/server/internal/store"
	"github.com/roxy-media/roxy/server/internal/trickplay"
)

// newBareServer is a server as it is at its very first start: no account.
func newBareServer(t *testing.T) (http.Handler, *store.Store, *config.Config) {
	t.Helper()
	data := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DataDir: data}
	safefs.Init(data)
	st, err := store.Open(filepath.Join(data, "roxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ff := &ffmpeg.FFmpeg{}
	return NewRouter(cfg, st, library.New(st, nil, cfg), ff, auth.New(st), nil, trickplay.New(st, ff, cfg), nil), st, cfg
}

// setupTry posts a first-time setup from addr and returns the answer.
func setupTry(t *testing.T, h http.Handler, addr, code string, as ...string) *httptest.ResponseRecorder {
	t.Helper()
	name := "owner"
	if len(as) > 0 {
		name = as[0]
	}
	body := `{"name":` + mustJSON(t, name) + `,"password":"a long enough password 1","code":` + mustJSON(t, code) + `,"device":"test","deviceId":"d1","version":"1"}`
	r := httptest.NewRequest("POST", "/v1/setup/admin", strings.NewReader(body))
	r.RemoteAddr = addr
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func setupSays(t *testing.T, h http.Handler, addr string) map[string]any {
	t.Helper()
	r := httptest.NewRequest("GET", "/v1/setup", nil)
	r.RemoteAddr = addr
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("GET /v1/setup: %d %s", rec.Code, rec.Body.String())
	}
	return out
}

// A new server could be claimed by whoever reached it first from an address that looks
// local: a guest on the Wi-Fi, and where the real address is hidden from the server
// (Docker Desktop, a plain tunnel) anybody at all. Setup asks for the code the server
// wrote into its log and data folder, which only its owner can read.
func TestSetupAsksForTheServersCode(t *testing.T) {
	h, _, cfg := newBareServer(t)
	const home = "192.168.1.50:4000"
	file := filepath.Join(cfg.DataDir, setupCodeFile)
	read := func() string {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("the setup code's file: %v", err)
		}
		return strings.TrimSpace(string(b))
	}
	code := read()
	if len(code) != setupCodeLength+1 || code[setupCodeLength/2] != '-' {
		t.Fatalf("the code %q is not XXXX-XXXX", code)
	}
	if fi, err := os.Stat(file); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the code's file must be the owner's alone: %v %v", fi.Mode().Perm(), err)
	}
	says := setupSays(t, h, home)
	if says["required"] != true || says["allowed"] != true || says["code"] != true {
		t.Fatalf("a new server says %v, want setup required, allowed from home, and a code asked for", says)
	}
	if b, _ := json.Marshal(says); strings.Contains(string(b), code) || strings.Contains(string(b), strings.ReplaceAll(code, "-", "")) {
		t.Fatal("the public setup answer gives the code out")
	}
	// From the home network without the code, with a wrong one, with none: refused, and no account is made.
	for _, wrong := range []string{"", "AAAA-AAAA", code[:len(code)-1]} {
		rec := setupTry(t, h, home, wrong)
		if rec.Code != 403 || !strings.Contains(rec.Body.String(), `"code":"setup_code"`) {
			t.Fatalf("setup with the code %q: %d %s, want 403 setup_code", wrong, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), code) {
			t.Fatal("the refusal gives the code out")
		}
	}
	if setupSays(t, h, home)["required"] != true {
		t.Fatal("a refused setup made an account")
	}
	// The code as a person types it: small letters, no dash, a space.
	typed := " " + strings.ToLower(strings.ReplaceAll(code, "-", " "))
	if rec := setupTry(t, h, home, typed); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"token"`) {
		t.Fatalf("setup with the right code: %d %s", rec.Code, rec.Body.String())
	}
	// Done: the code is gone with its file, and the door is shut.
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("the code's file is still there after setup (%v)", err)
	}
	says = setupSays(t, h, home)
	if says["required"] != false || says["code"] != false {
		t.Fatalf("after setup the server says %v", says)
	}
	if rec := setupTry(t, h, home, code, "somebody else"); rec.Code != 409 {
		t.Fatalf("a second setup: %d, want 409", rec.Code)
	}
}

// The code cannot be guessed at: after a few wrong ones there is a new code, and the old
// one, had it been next on the guesser's list, is wrong too.
func TestSetupCodeIsReplacedAfterWrongTries(t *testing.T) {
	h, _, cfg := newBareServer(t)
	const home = "192.168.1.51:4000"
	file := filepath.Join(cfg.DataDir, setupCodeFile)
	first, _ := os.ReadFile(file)
	for i := 0; i < setupCodeTries; i++ {
		if rec := setupTry(t, h, home, "ZZZZ-ZZZZ"); rec.Code != 403 {
			t.Fatalf("wrong try %d: %d", i, rec.Code)
		}
	}
	second, _ := os.ReadFile(file)
	if len(second) == 0 || string(second) == string(first) {
		t.Fatalf("the code was not replaced after %d wrong tries", setupCodeTries)
	}
	if rec := setupTry(t, h, home, strings.TrimSpace(string(first))); rec.Code != 403 {
		t.Fatalf("the replaced code still works: %d", rec.Code)
	}
	if rec := setupTry(t, h, home, strings.TrimSpace(string(second))); rec.Code != 200 {
		t.Fatalf("the new code: %d %s", rec.Code, rec.Body.String())
	}
}

// The code does not open the door to somebody who is not on the home network.
func TestSetupCodeIsNotEnoughFromOutside(t *testing.T) {
	h, _, cfg := newBareServer(t)
	code, _ := os.ReadFile(filepath.Join(cfg.DataDir, setupCodeFile))
	if rec := setupTry(t, h, "203.0.113.9:4000", strings.TrimSpace(string(code))); rec.Code != 403 || strings.Contains(rec.Body.String(), "setup_code") {
		t.Fatalf("setup from the internet with the code: %d %s, want the home-network refusal", rec.Code, rec.Body.String())
	}
}

// A server that has its account makes no code, and clears one left in its data folder.
func TestNoSetupCodeOnceThereIsAnAccount(t *testing.T) {
	dir := t.TempDir()
	safefs.Init(dir)
	stale := filepath.Join(dir, setupCodeFile)
	if err := os.WriteFile(stale, []byte("OLD1-CODE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newSetupCode(dir, false)
	if c.required() || !c.matches("") {
		t.Fatal("a server with an account asks for a setup code")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("a left-over code file was kept")
	}
	// Codes differ from start to start.
	a, b := newSetupCode(t.TempDir(), true), newSetupCode(t.TempDir(), true)
	if a.code == b.code || a.code == "" {
		t.Fatalf("two servers made the codes %q and %q", a.code, b.code)
	}
	for _, r := range strings.ReplaceAll(a.code, "-", "") {
		if !strings.ContainsRune(setupCodeAlphabet, r) {
			t.Fatalf("the code %q has a letter outside its alphabet", a.code)
		}
	}
}
