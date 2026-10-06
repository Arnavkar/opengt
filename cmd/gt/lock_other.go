//go:build !unix

package main

import "os"

// ponytail: catalog locking is a no-op off Unix; the atomic rename in
// stack.go still guarantees whole-file reads there. Add a real lock (e.g.
// LockFileEx) when a Windows interop race shows up in practice.
func lockFile(*os.File) error   { return nil }
func unlockFile(*os.File) error { return nil }
