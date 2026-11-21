---
name: go-performance-profiler
description: Analyze Go code for CPU bottlenecks, heap escapes, GC pressure, and lock contention, and generate pprof workflows and microbenchmarks. Use when the user asks to optimize Go performance, reduce Go memory allocations, inspect escape analysis, or profile Go services with pprof.
---

# go-performance-profiler

Diagnose and eliminate CPU bottlenecks, excessive heap allocations, garbage collection pressure, and synchronization contention in Go applications using empirical benchmarking and `pprof`.

## When to Use

- The user shares slow or memory-heavy Go code and asks how to optimize throughput or latency.
- The user wants `testing.B` microbenchmarks (`-benchmem`), escape analysis explanations, or `pprof` profiling commands.
- The user is optimizing hot paths such as serialization, log/metrics ingestion, network handlers, or tight loops.

## Profiling and Optimization Workflow

1. **Establish a Baseline Benchmark First**
   - Write a realistic `func BenchmarkXxx(b *testing.B)` that calls `b.ReportAllocs()`, resets the timer after setup (`b.ResetTimer()`), and assigns outputs to a package-level sink (`var Sink any`) so the compiler cannot dead-code-eliminate the hot loop.
   - Provide exact measurement commands (`go test -bench=. -benchmem -count=6` and `benchstat`) before recommending invasive changes.

2. **Eliminate Hot-Path Heap Allocations**
   - Flag values escaping to the heap due to interface boxing (`any` / `fmt.Sprintf`), returning pointers to local stack structs unnecessarily, or capturing variables in closures.
   - Recommend preallocating slices and maps (`make([]T, 0, expectedCap)`), reusing buffers via `sync.Pool` (with proper `Reset()` and size caps to avoid retaining oversized buffers), and replacing `fmt.Sprintf` with `strconv` or `strings.Builder`.

3. **Reduce GC Pressure and Cache Misses**
   - Prefer contiguous value slices (`[]Item`) over pointer slices (`[]*Item`) when items are small and short-lived to reduce pointer chasing and GC scan overhead.
   - Reorder struct fields from largest alignment to smallest to pack hot structs into fewer CPU cache lines.

4. **Resolve Synchronization and Contention Bottlenecks**
   - Identify global `sync.Mutex` bottlenecks on read-heavy or sharded workloads. Recommend `sync.RWMutex`, `atomic` primitives (`atomic.Int64`, `atomic.Pointer`), or sharded locks/maps keyed by hash.
   - Flag unbuffered or undersized channels causing goroutine stalls in high-throughput pipelines.

## Output Format

Organize findings into three sections:

1. **Bottleneck Diagnosis**: Specific lines or patterns causing allocations, CPU cycles, or lock contention, noting expected compiler escape behavior (`-gcflags="-m"`).
2. **Optimized Implementation**: Drop-in replacement Go code prioritizing zero-allocation or low-allocation execution.
3. **Benchmark and `pprof` Verification**: A ready-to-run `Benchmark*` function and exact `go test` / `go tool pprof` commands to prove the speedup and allocation drop.

## Gotchas

- Never optimize cold initialization paths at the expense of readability; focus strictly on verified hot paths.
- Warn against pooling oversized slices in `sync.Pool` without a capacity ceiling (`if cap(buf) > maxCap { return }`), which causes silent RSS memory bloat.
