package api

import (
	"crypto/rand"
	"crypto/subtle"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/roxy-media/roxy/server/internal/safefs"
)

// setupCode is what proves that the person setting a new server up is its owner.
//
// A server with no account lets "the first person on the home network" make the admin.
// The home network is judged by the address a request comes from, and that says less
// than it seems: a guest on the Wi-Fi is on it; and wherever the real address is hidden
// from the server everybody is (Docker Desktop hands every client over from its own
// gateway, IPv6 through Docker's proxy likewise, a plain TCP tunnel from the internet
// arrives from the tunnel's local end). Whoever got there first owned the server.
//
// So the server makes a code when it starts without an account, and setup asks for it.
// It is written where only the owner looks: the server's log, and a file in its data
// folder. Nothing on the network gives it out.
type setupCode struct {
	mu    sync.Mutex
	code  string // "" = none wanted (the server has an account)
	path  string
	wrong int
}

const (
	setupCodeFile = "setup-code.txt"
	// Letters and digits that are not taken for one another when read off a screen.
	setupCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	setupCodeLength   = 8
	// After this many wrong codes a new one is made: guessing gets nowhere.
	setupCodeTries = 10
)

// newSetupCode makes the code for a server that has no account (wanted), and clears a
// left-over one for a server that has.
func newSetupCode(dataDir string, wanted bool) *setupCode {
	c := &setupCode{}
	if dataDir != "" {
		c.path = filepath.Join(dataDir, setupCodeFile)
	}
	if !wanted {
		if c.path != "" {
			_ = safefs.Remove(c.path)
		}
		return c
	}
	c.renew("this server has no account yet")
	return c
}

func (c *setupCode) renew(why string) {
	buf := make([]byte, setupCodeLength)
	if _, err := rand.Read(buf); err != nil {
		panic("setup code: no random numbers: " + err.Error()) // crypto/rand does not fail on the systems Roxy runs on
	}
	var b strings.Builder
	for i, x := range buf {
		if i == setupCodeLength/2 {
			b.WriteByte('-')
		}
		// 256 is not a multiple of 31: the first eight letters come up a little more
		// often (9 in 256 against 8). Of no use to anyone at ten tries a code.
		b.WriteByte(setupCodeAlphabet[int(x)%len(setupCodeAlphabet)])
	}
	c.code, c.wrong = b.String(), 0
	if c.path != "" {
		if err := safefs.WriteFile(c.path, []byte(c.code+"\n"), 0o600); err != nil {
			slog.Warn("setup: the code could not be written to its file; it is in this log only", "file", c.path, "err", err)
		}
	}
	slog.Warn("setup: "+why+". Setup asks for this code", "code", c.code, "file", c.path)
}

// required reports whether setup asks for a code.
func (c *setupCode) required() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.code != ""
}

// matches checks a typed code: letters in either case, the dash and spaces left out or
// not. Too many wrong ones and the code is replaced.
func (c *setupCode) matches(typed string) bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.code == "" {
		return true
	}
	clean := func(s string) []byte {
		out := make([]byte, 0, setupCodeLength)
		for _, r := range strings.ToUpper(s) {
			if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				out = append(out, byte(r))
			}
		}
		return out
	}
	if subtle.ConstantTimeCompare(clean(typed), clean(c.code)) == 1 {
		return true
	}
	c.wrong++
	if c.wrong >= setupCodeTries {
		c.renew("too many wrong setup codes were tried, so a new one was made")
	}
	return false
}

// done: the server has its account; the code and its file are gone.
func (c *setupCode) done() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.code = ""
	if c.path == "" {
		return
	}
	if err := safefs.Remove(c.path); err != nil && !os.IsNotExist(err) {
		slog.Warn("setup: the code's file could not be removed", "file", c.path, "err", err)
	}
}
