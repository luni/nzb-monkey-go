package main

import (
	"errors"
	"testing"

	"github.com/Tensai75/fslock"
)

// acquireLock must acquire, release and re-acquire the lock file
func TestAcquireLock(t *testing.T) {
	tempPath = t.TempDir()

	lock, err := acquireLock()
	if err != nil {
		t.Fatalf("acquireLock: %v", err)
	}

	// while held, a second lock on the same file must report ErrLocked
	second := fslock.New(directSearchLockPath())
	if err := second.TryLock(); !errors.Is(err, fslock.ErrLocked) {
		t.Errorf("second TryLock err = %v, want ErrLocked", err)
	}

	if err := lock.Unlock(); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	// after release, acquiring must succeed again
	lock2, err := acquireLock()
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	if err := lock2.Unlock(); err != nil {
		t.Fatalf("unlock after re-acquire: %v", err)
	}
}
