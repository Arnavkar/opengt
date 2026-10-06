package main

import (
	"os"
	"path/filepath"
)

// withCatalogLock takes the flock that gh-stack 0.2.0 uses for catalog
// interop (`gh-stack.lock` beside the state file, internal/stack/lock.go).
// gt takes it around its own writes so they cannot interleave with a running
// gh-stack mutation. An absent or already-released lock is cheap; the kernel
// drops flocks when a process exits, so a crashed holder never wedges this.
// Returns the unlock, which closes the file (releasing the lock on every OS).
func withCatalogLock(catalogPath string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(filepath.Dir(catalogPath), "gh-stack.lock"),
		os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = unlockFile(f)
		_ = f.Close()
	}, nil
}
