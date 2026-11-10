package config

import (
	"strings"
	"testing"
)

// A switch or a number that cannot be read stops the start and says which one: the
// default used to be taken in silence, so ROXY_WATCH=flase kept watching.
func TestUnreadableSettingIsAnError(t *testing.T) {
	for key, value := range map[string]string{
		"ROXY_WATCH":          "flase",
		"ROXY_SCAN_ON_START":  "nope",
		"ROXY_DEBUG":          "2",
		"ROXY_DISCOVERY_PORT": "8096/udp",
	} {
		t.Run(key, func(t *testing.T) {
			t.Setenv("ROXY_DATA_DIR", t.TempDir())
			t.Setenv(key, value)
			_, err := Load()
			if err == nil {
				t.Fatalf("%s=%s was accepted", key, value)
			}
			if !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), value) {
				t.Errorf("the error does not name the setting and its value: %v", err)
			}
		})
	}
}

func TestSwitchSpellings(t *testing.T) {
	for value, want := range map[string]bool{
		"true": true, "1": true, "TRUE": true, "yes": true, "On": true, " true ": true,
		"false": false, "0": false, "no": false, "OFF": false,
	} {
		t.Setenv("ROXY_DATA_DIR", t.TempDir())
		t.Setenv("ROXY_WATCH", value)
		t.Setenv("ROXY_DISCOVERY_PORT", " 0 ")
		c, err := Load()
		if err != nil {
			t.Fatalf("ROXY_WATCH=%q: %v", value, err)
		}
		if c.Watch != want {
			t.Errorf("ROXY_WATCH=%q read as %v", value, c.Watch)
		}
		if c.DiscoveryPort != 0 {
			t.Errorf("ROXY_DISCOVERY_PORT=\" 0 \" read as %d", c.DiscoveryPort)
		}
	}
}

// Unset and empty settings keep their defaults.
func TestDefaults(t *testing.T) {
	t.Setenv("ROXY_DATA_DIR", t.TempDir())
	t.Setenv("ROXY_WATCH", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.Watch || !c.ScanOnStart || c.Debug || c.DiscoveryPort != 8096 {
		t.Errorf("defaults: watch %v, scan on start %v, debug %v, discovery port %d", c.Watch, c.ScanOnStart, c.Debug, c.DiscoveryPort)
	}
}
