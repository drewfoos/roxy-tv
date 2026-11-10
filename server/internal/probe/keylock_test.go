package probe

import (
	"testing"
	"time"
)

// A caller that arrives as the holder lets go must still wait for whoever was already
// waiting: forgetting the name on release gave the newcomer a lock of its own.
func TestKeyLocksHoldAcrossAHandOver(t *testing.T) {
	var k keyLocks
	releaseA := k.lock("x")

	bIn, bOut := make(chan struct{}), make(chan struct{})
	go func() {
		release := k.lock("x")
		close(bIn)
		<-bOut
		release()
	}()
	for deadline := time.Now().Add(5 * time.Second); k.users("x") < 2; {
		if time.Now().After(deadline) {
			t.Fatal("the second caller never queued")
		}
		time.Sleep(time.Millisecond)
	}
	releaseA()
	<-bIn

	cIn := make(chan struct{})
	go func() {
		release := k.lock("x")
		close(cIn)
		release()
	}()
	select {
	case <-cIn:
		t.Fatal("a third caller got the lock while the second still held it")
	case <-time.After(100 * time.Millisecond):
	}
	close(bOut)
	select {
	case <-cIn:
	case <-time.After(5 * time.Second):
		t.Fatal("the third caller never got the lock")
	}
	for deadline := time.Now().Add(5 * time.Second); k.users("x") != 0; {
		if time.Now().After(deadline) {
			t.Fatalf("the name is still kept with %d users", k.users("x"))
		}
		time.Sleep(time.Millisecond)
	}
	if len(k.m) != 0 {
		t.Fatalf("names kept after everyone let go: %d", len(k.m))
	}

	// Another name is never held up.
	done := make(chan struct{})
	hold := k.lock("y")
	go func() { k.lock("z")(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("one name's lock held up another's")
	}
	hold()
}
