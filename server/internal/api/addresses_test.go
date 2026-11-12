package api

import (
	"reflect"
	"testing"

	"github.com/roxy-media/roxy/server/internal/config"
)

// The app signed in at home must be able to find the server away from home: the server
// lists every address it is set to answer at, the advertised (home) one first.
func TestAddressesListsTheAdvertisedURLThenTheOthersOnce(t *testing.T) {
	s := &Server{cfg: &config.Config{
		AdvertiseURL: "http://192.168.1.50:8097",
		OtherURLs:    []string{"http://100.64.0.7:8097", "http://192.168.1.50:8097", "http://box.example.ts.net:8097"},
	}}
	want := []string{"http://192.168.1.50:8097", "http://100.64.0.7:8097", "http://box.example.ts.net:8097"}
	if got := s.addresses(); !reflect.DeepEqual(got, want) {
		t.Fatalf("addresses = %v, want %v", got, want)
	}
	if got := (&Server{cfg: &config.Config{}}).addresses(); len(got) != 0 {
		t.Fatalf("nothing set: addresses = %v, want none", got)
	}
}
