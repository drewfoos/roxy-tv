package probe

import "sync"

// keyLocks is one lock per name, kept only while somebody holds or waits for it.
//
// A plain map of mutexes that forgot a name as its holder let go did not hold: someone
// already waiting kept the old mutex while the next caller made a new one, and the two
// ran together.
type keyLocks struct {
	mu sync.Mutex
	m  map[string]*keyLock
}

type keyLock struct {
	mu    sync.Mutex
	users int // holding or waiting
}

// lock waits for the name's lock and returns what releases it.
func (k *keyLocks) lock(name string) (unlock func()) {
	k.mu.Lock()
	if k.m == nil {
		k.m = map[string]*keyLock{}
	}
	l := k.m[name]
	if l == nil {
		l = &keyLock{}
		k.m[name] = l
	}
	l.users++
	k.mu.Unlock()

	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		k.mu.Lock()
		if l.users--; l.users == 0 {
			delete(k.m, name)
		}
		k.mu.Unlock()
	}
}
