---
name: verify-gt
description: >-
  Prove opengt `gt` CLI behavior with the Go integration suite. Use when
  verifying gt, checking a stack command, or before changing cmd/gt.
---

# Verify `gt`

The suite is `go test -tags=integration ./test/integration/`. It is not
`gt-sandbox`. It does not prompt and does not open a TTY, browser, or
Terminal window.

```sh
go test -tags=integration -timeout 15m ./test/integration/
```

Harness: `test/integration/harness_test.go`. Each test gets its own
`t.TempDir()` repo and a bare `origin` on disk. `GH_TOKEN` is unset.
Stdin is closed so a picker cannot block.

What is covered and what is not: `test/integration/README.md`.
Expected per-command behavior: `features/`. Gaps that still need a
GitHub token or a TUI: `features/gaps.md`.

Picker chrome (`gt checkout` with no args) is unit-tested in
`cmd/gt/forest_test.go` and `cmd/gt/picker_test.go`. Named
`gt checkout <branch>` is an integration test. Do not open Terminal to
"show" the picker.

Cleanup is `t.TempDir()`. There is no evidence directory to keep.
