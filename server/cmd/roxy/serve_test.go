package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// Stopping the server ends the requests it is serving: a stream's context is cancelled
// (which stops its ffmpeg), and serve returns. Shutdown alone waited for streams, which
// are never idle, and gave up after 5 s with them still running.
func TestStoppingTheServerEndsStreams(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ended := make(chan struct{})
	started := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done() // a stream: runs until its request ends
		close(ended)
	})
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- serveHTTP(ctx, ln, h) }()
	resp, err := http.Get("http://" + ln.Addr().String() + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	go io.Copy(io.Discard, resp.Body)
	<-started
	stopped := time.Now()
	cancel() // SIGTERM
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("the stream's request was not cancelled when the server stopped")
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serveHTTP: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serveHTTP did not return")
	}
	if took := time.Since(stopped); took > 4*time.Second {
		t.Errorf("stopping took %v: it waited for the stream instead of ending it", took)
	}
}

// serveHTTP returns only when the requests in flight have finished (or the grace period
// is over): the caller closes the database next. It used to return the moment the stop
// began, so a request still writing lost the database under it.
func TestServeWaitsForRequestsInFlight(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	finished := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(400 * time.Millisecond) // work that does not stop with its request, like a write to the database
		close(finished)
	})
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- serveHTTP(ctx, ln, h) }()
	go func() {
		if resp, err := http.Get("http://" + ln.Addr().String() + "/"); err == nil {
			resp.Body.Close()
		}
	}()
	<-started
	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serveHTTP: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serveHTTP did not return")
	}
	select {
	case <-finished:
	default:
		t.Error("serveHTTP returned while a request was still being handled")
	}
}

// A request that never finishes does not hold the stop up past the grace period.
func TestServeGivesUpAfterTheGracePeriod(t *testing.T) {
	old := shutdownGrace
	shutdownGrace = 200 * time.Millisecond
	defer func() { shutdownGrace = old }()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	})
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- serveHTTP(ctx, ln, h) }()
	go func() {
		if resp, err := http.Get("http://" + ln.Addr().String() + "/"); err == nil {
			resp.Body.Close()
		}
	}()
	<-started
	cancel()
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("serveHTTP waited for a request that never ends")
	}
}
