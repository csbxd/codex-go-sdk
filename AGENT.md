## Direction Of Change

- Prefer the real protocol and server behavior over client-side emulation.
- Prefer concrete types and direct representations over encoded helper layers.
- Prefer current stdlib / current Go features over local wrappers.
- Prefer smaller files with clear responsibility boundaries.
- Prefer the minimum synchronization needed for real runtime behavior.
- Prefer deterministic lifecycle handling over timing-based fixes.

## Rules

- Use stdlib helpers directly when the current Go version already provides them.
  Example: use `errors.AsType`, do not add local `errorAs` / `asJSONRPCError` wrappers.

- Do not add client-side caches or shadow state to smooth over server behavior unless the protocol requires it.
  If the app-server should answer a repeated request, forward it and handle the real response.

- Keep public RPC methods thin.
  `Initialize`, `Notify`, `Request`, `Call`, notification streaming helpers, and typed `Thread*` / `Turn*` / `Model*` wrappers belong in `codex/client_rpc.go`.
  Process startup, shutdown, stdio transport, pending-request bookkeeping, and terminal transport errors belong in `codex/client.go`.

- Prefer simple fields over proactive locking.
  Do not add mutexes, atomics, or extra channels just because a value looks shared.
  Add synchronization only when there is a concrete runtime race after startup.

- Child-process shutdown must be deterministic.
  Coordinate `stdout`, `stderr`, and `Wait()` in one clear shutdown path.
  Do not add sleep/retry polling just to “eventually” capture stderr tails.

- Delete temporary wrappers and superseded helpers during refactors.
  The codebase trend is toward fewer layers, not more compatibility shims.

- Use index-based iteration for slices in SDK code and tests when touching existing loops.
  Prefer `for i := range slice { v := slice[i] ... }`.
  Keep `range` over maps as map iteration.

- Keep support code small and local.
  If a helper only exists to serve one transport or RPC flow, keep it near that flow instead of building a reusable abstraction first.

- Treat generated protocol files as generated artifacts.
  Avoid manual edits in `codex/protocol/generated.go` and `codex/protocol/registry_generated.go` unless the task is specifically about schema / generator changes.

## Default Bias

If two designs both work, choose the one with:

- fewer local abstractions
- fewer synchronization primitives
- more direct use of protocol-native types
- clearer file boundaries
- less speculative compatibility logic