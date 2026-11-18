---
name: go-pr-reviewer
description: Audit Go code, pull requests, and diffs for concurrency bugs, context leaks, error handling issues, and hot-path allocations. Use when the user asks to review Go code, inspect a Golang PR, check a Go diff, or find bugs and performance traps in Go files.
---

# go-pr-reviewer

Perform rigorous, production-focused code reviews of Go diffs, functions, and pull requests. Prioritize correctness, concurrency safety, resource lifecycles, and allocation efficiency over cosmetic formatting.

## When to Use

- The user shares Go code, a `git diff`, or a pull request and asks for a code review or bug audit.
- The user asks to check Go code for race conditions, goroutine leaks, context misuse, or hot-path performance issues.
- The user wants actionable PR feedback with severity rankings and concrete replacement snippets.

## Review Workflow

1. **Audit Concurrency and Goroutine Lifecycles**
   - Verify every spawned `go func()` has a deterministic termination path via `ctx.Done()`, a closed channel, or a bounded worker group (`errgroup.Group` or `sync.WaitGroup`). Flag fire-and-forget goroutines that can leak under load.
   - Confirm channel ownership: only the producer closes a channel, never a consumer. Check `select` blocks for missing `ctx.Done()` branches that can deadlock on full or unbuffered channels.
   - Check shared map access under concurrency, flag structs containing `sync.Mutex` or `sync.RWMutex` passed by value, and flag mutexes held across network or disk I/O.

2. **Verify Context Propagation and Resource Cleanup**
   - Ensure `ctx context.Context` is the first parameter on all I/O, database, and RPC boundaries rather than stored inside structs or replaced mid-call with `context.Background()`.
   - Confirm `defer cancel()`, `defer rows.Close()`, and `defer file.Close()` occur immediately after checking errors and never inside loops where defers accumulate until function return.

3. **Check Error Handling and Boundary Contracts**
   - Verify errors carry operational context via `fmt.Errorf("action %s: %w", id, err)` without redundant prefixes such as `"failed to"`. Ensure callers inspect errors with `errors.Is` or `errors.As` rather than string matching.
   - Flag silent error drops (`_ = fn()`) and unchecked `Close()` calls on writable files or network streams where flush errors cause silent data loss.
   - Ensure `panic` is restricted to unrecoverable startup invariant checks and never used for runtime or library error flow.

4. **Inspect Hot-Path Allocations and Idiomatic Flow**
   - Inside loops or high-throughput handlers, flag avoidable heap allocations: `fmt.Sprintf` where `strconv` or `strings.Builder` suffices, un-preallocated slices (`make([]T, 0, n)`), and unintentional pointer escapes.
   - Check for happy-path left alignment using early guard clauses (`if err != nil { return err }`) rather than deeply nested `else` blocks.

5. **Evaluate Test Coverage and Race Safety**
   - Check that new logic includes table-driven tests covering boundary and error paths, uses `t.Cleanup()` for teardown, and is safe under `go test -race`.

## Output Format

Group findings strictly by severity and omit empty categories:

- **Must-Fix**: Bugs, data races, goroutine or resource leaks, silent data corruption, or broken error contracts.
- **Should-Fix**: Suboptimal context handling, hot-path allocation traps, missing edge-case tests, or brittle abstractions.
- **Nit**: Minor readability or idiomatic cleanups.

For every finding, provide:
1. Exact location (file and line or function name).
2. Why it fails in production (concise technical mechanism).
3. A minimal, drop-in Go replacement snippet.

## Gotchas

- Do not nitpick formatting already enforced by `gofmt` or `golangci-lint` unless it obscures a real bug.
- Do not invent hypothetical issues when code is already sound; if a diff has no `Must-Fix` or `Should-Fix` items, state clearly that it is safe to merge and summarize what was verified.
