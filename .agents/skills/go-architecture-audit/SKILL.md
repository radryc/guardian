---
name: go-architecture-audit
description: Evaluate Go package structure, interface design, struct memory layout, coupling, and testability. Use when the user asks for an architectural review of a Go codebase, wants to refactor Go packages or interfaces, or asks how to decouple and structure Go services and libraries.
---

# go-architecture-audit

Evaluate Go package boundaries, abstraction design, dependency direction, and data layout to keep services and libraries cohesive, testable, and fast.

## When to Use

- The user asks to review the architecture, package layout, or module boundaries of a Go project.
- The user wants advice on refactoring bloated Go interfaces, dependency injection, or circular imports.
- The user asks to audit Go structs for memory alignment, receiver consistency, or API ergonomics.

## Audit Workflow

1. **Evaluate Package Boundaries and Dependency Direction**
   - Flag generic catch-all packages (`util`, `common`, `helpers`, `models`) that accumulate unrelated dependencies and invite import cycles. Recommend domain-oriented packages organized by business capability or data ownership.
   - Verify dependency arrows point inward toward core domain types rather than coupling domain logic to transport (`net/http`, gRPC) or storage drivers.

2. **Audit Interface Granularity and Ownership**
   - Apply the Go proverb: accept interfaces, return structs. Flag large producer-side interfaces (more than 3 to 4 methods) that force mocks to stub unused behavior.
   - Recommend narrow 1 to 3 method interfaces defined in the consumer package that uses them, composing larger behaviors only when a caller genuinely requires all methods together.

3. **Inspect Struct Design, Receivers, and Memory Layout**
   - Check receiver consistency on each type: avoid mixing value and pointer receivers. Require pointer receivers when methods mutate state, embed `sync.Mutex` or `sync.Once`, or operate on large structs.
   - Check zero-value usability (`var buf bytes.Buffer` readiness) and functional options (`WithTimeout(...)`) when constructors exceed 3 to 4 configuration parameters.
   - On hot-path or high-cardinality structs, check field ordering from largest alignment to smallest (`int64`/pointers, `int32`/`float32`, `int16`, `bool`/`byte`) to eliminate wasted padding bytes.

4. **Assess Concurrency Topology and Testability**
   - Identify hidden global state, package-level singletons, or hardcoded `time.Now()` and network calls that block deterministic unit testing.
   - Recommend injecting small clock, storage, or transport seams at package boundaries so core logic runs fast without external infrastructure.

## Output Format

Structure the audit into three sections:

1. **Architectural Strengths**: What already works well and should be preserved.
2. **Structural Findings**: Ranked by impact (High, Medium, Low), explaining the coupling, maintenance, or performance cost of each issue.
3. **Refactoring Plan**: Concrete steps with before/after Go code showing the cleaner package boundary, interface, or struct layout.

## Gotchas

- Do not recommend enterprise Java or Spring-style layers (such as separate `controller`, `service`, `repository`, and `dto` packages) for small or medium Go services; prefer flat, cohesive domain packages.
- Do not introduce interfaces prematurely where only one concrete implementation exists and unit tests can run directly against it.
