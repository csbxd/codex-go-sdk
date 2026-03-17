# codex-go-sdk

Experimental Go SDK for `codex app-server` JSON-RPC v2 over stdio.

This repository keeps the SDK implementation under [`codex/`](./codex). It launches `codex app-server`, performs the `initialize` / `initialized` handshake, exposes typed request and notification types derived from schema files downloaded from `openai/codex` by default, and provides a small synchronous client surface for common thread, turn, and model workflows.

## Status

- Transport: stdio only
- Protocol target: Codex `app-server` JSON-RPC v2
- Scope: typed app-server client, not a direct CLI/JSONL wrapper
- Runtime packaging: the Go module does not bundle a `codex` binary; use `Config.CodexBin` or ensure `codex` is on `PATH`

## Install

```bash
go get github.com/csbxd/codex-go-sdk/codex
```

## Quickstart

```go
package main

import (
  "context"
  "fmt"
  "log"

  "github.com/csbxd/codex-go-sdk/codex"
  "github.com/csbxd/codex-go-sdk/codex/protocol"
)

func main() {
  ctx := context.Background()

  client := codexappserver.NewClient(codexappserver.Config{})
  defer client.Close()

  if _, err := client.Open(ctx); err != nil {
    log.Fatal(err)
  }

  started, err := client.ThreadStart(ctx, &protocol.ThreadStartParams{
    Model: codexappserver.Ptr("gpt-5"),
  })
  if err != nil {
    log.Fatal(err)
  }

  turnStarted, err := client.TurnStartText(
    ctx,
    started.Thread.ID,
    "Say hello in one sentence.",
    &protocol.TurnStartParams{
      Effort: codexappserver.Ptr(protocol.ReasoningEffortMedium),
    },
  )
  if err != nil {
    log.Fatal(err)
  }

  completed, err := client.WaitForTurnCompleted(ctx, turnStarted.Turn.ID)
  if err != nil {
    log.Fatal(err)
  }

  fmt.Println(completed.Turn.Status)
}
```

## Core API

- `NewClient(Config)` constructs the client.
- `(*Client).Open(ctx)` starts the child process, sends `initialize`, and emits `initialized`.
- `(*Client).Request(ctx, method, params, out)` is the generic request path for methods that do not yet have a dedicated wrapper.
- Typed wrappers are available for:
  `thread/start`, `thread/resume`, `thread/list`, `thread/read`, `thread/fork`, `thread/archive`, `thread/unarchive`, `thread/name/set`, `thread/compact/start`,
  `turn/start`, `turn/steer`, `turn/interrupt`,
  and `model/list`.
- `TextInput`, `TurnStartText`, and `TurnSteerText` cover the common text-only turn flows.
- When your local Codex config pins an aggressive default reasoning effort, pass an explicit supported value such as `protocol.ReasoningEffortMedium` for `gpt-5` turns.
- `NextNotification(ctx)` returns typed payloads when a method exists in the generated notification registry.
- `WaitForTurnCompleted(ctx, turnID)` and `StreamUntilMethods(ctx, methods...)` help consume the notification stream.
- Status helpers in `codex/protocol/` expose `IsTerminal` / `CanTransitionTo` for turn, item, hook, plan, and collab statuses, plus `ParseThreadStatus` for the raw `ThreadStatus` union.
- `RetryOnOverload` retries only the documented overload JSON-RPC error (`-32001`) and leaves all other retry policy explicit.

Generated protocol types live under the [`codex/protocol/`](./codex/protocol) package.

## Status machines

Most status enums in [`codex/protocol/`](./codex/protocol) expose two helper methods:

- `status.IsTerminal()`
- `status.CanTransitionTo(next)`

For these helpers, terminal means "no forward transition is allowed anymore", and self-transitions are treated as valid (`status.CanTransitionTo(status) == true`).

`ThreadStatus` is the exception. In the wire schema it is a tagged union, so the generated Go type is raw JSON. Decode it with `protocol.ParseThreadStatus`, `Thread.StatusState()`, or `ThreadStatusChangedNotification.StatusState()`.

```go
read, err := client.ThreadRead(ctx, &protocol.ThreadReadParams{
  ThreadId: threadID,
})
if err != nil {
  return err
}

state, err := read.Thread.StatusState()
if err != nil {
  return err
}

if state.IsActive() && state.HasActiveFlag(protocol.ThreadActiveFlagWaitingOnApproval) {
  // The server is blocked on at least one approval request.
}

if completed.Turn.Status.IsTerminal() {
  // The turn reached completed, failed, or interrupted.
}
```

### `ThreadStatus`

Decoded wire shapes:

- `{"type":"notLoaded"}`
- `{"type":"idle"}`
- `{"type":"systemError"}`
- `{"type":"active","activeFlags":[...]}`

`activeFlags` is only valid for `type == "active"`:

- `waitingOnApproval`: the server currently has one or more pending approval requests
- `waitingOnUserInput`: the server currently has one or more pending user-input requests

Unlike the item-level statuses below, `ThreadStatus` is a server-derived runtime snapshot, not a strict client-owned finite-state machine:

- `notLoaded`: the thread is not loaded in the runtime
- `active`: the thread is loaded and either a turn is running or the runtime is waiting on approval/user input
- `systemError`: the thread is loaded, not active, and the runtime is in a system-error state
- `idle`: the thread is loaded, not active, and has no system error

### Enum transition rules

| Status type | Terminal states | Allowed forward transitions |
| --- | --- | --- |
| `TurnStatus` | `completed`, `failed`, `interrupted` | `inProgress -> completed \| failed \| interrupted` |
| `CommandExecutionStatus` | `completed`, `failed`, `declined` | `inProgress -> completed \| failed \| declined` |
| `PatchApplyStatus` | `completed`, `failed`, `declined` | `inProgress -> completed \| failed \| declined` |
| `McpToolCallStatus` | `completed`, `failed` | `inProgress -> completed \| failed` |
| `DynamicToolCallStatus` | `completed`, `failed` | `inProgress -> completed \| failed` |
| `CollabAgentToolCallStatus` | `completed`, `failed` | `inProgress -> completed \| failed` |
| `HookRunStatus` | `completed`, `failed`, `blocked`, `stopped` | `running -> completed \| failed \| blocked \| stopped` |
| `CollabAgentStatus` | `completed`, `errored`, `shutdown`, `notFound` | `pendingInit -> running \| terminal`, `running -> terminal` |
| `TurnPlanStepStatus` | `completed` | `pending -> inProgress \| completed`, `inProgress -> completed` |

Approval-sensitive item states end in `declined` when the server requested approval and the client rejected it:

- `CommandExecutionStatusDeclined`
- `PatchApplyStatusDeclined`

## Configuration

`Config` supports:

- `CodexBin`: explicit path to the `codex` executable
- `LaunchArgsOverride`: full command override, useful for tests or custom launchers
- `ConfigOverrides`: repeated `--config key=value` arguments
- `CWD`: process working directory
- `Env`: environment overrides merged onto the current process environment
- `ClientName`, `ClientTitle`, `ClientVersion`: metadata sent in `initialize`
- `ExperimentalAPI`: populates `initialize.capabilities.experimentalApi`
- `RequestHandler`: callback for server-initiated approval and other JSON-RPC requests

If `RequestHandler` is not set, the SDK auto-accepts command-execution and file-change approval requests and returns `{}` for other server requests.

## Notifications and approvals

Known notification payloads are decoded using the generated method registry in [`codex/protocol/registry_generated.go`](./codex/protocol/registry_generated.go). Unknown methods, or methods whose payloads fail to decode into the expected generated type, are surfaced as `UnknownPayload` so callers still see the raw params.

Server-initiated requests are routed through `RequestHandler`. The SDK writes the returned value back as the JSON-RPC `result` for that request.

## Examples

- [`examples/quickstart`](./examples/quickstart)
- [`examples/streamnotifications`](./examples/streamnotifications)
- [`examples/requestapproval`](./examples/requestapproval)

## Maintainer workflow

Regenerate schema-derived artifacts:

```bash
cd /path/to/codex-go-sdk
go run ./cmd/updatesdkartifacts generate-types
```

By default the generator downloads schema inputs from `openai/codex` on the `main` branch at runtime. Optional overrides:

- `CODEX_GO_SCHEMA_REF=<git-ref>`
- `CODEX_GO_SCHEMA_REPO=<owner/repo>`
- `CODEX_GO_SCHEMA_BASE_URL=<raw-base-url>`
- `CODEX_GO_SCHEMA_ROOT=/path/to/local/schema/json`

Run tests:

```bash
cd /path/to/codex-go-sdk
go test ./...
```

Run the opt-in real `codex app-server` integration smoke tests:

```bash
cd /path/to/codex-go-sdk
CODEX_GO_INTEGRATION=1 go test -run Integration ./codex
```

Optional integration overrides:

- `CODEX_GO_INTEGRATION_CODEX_BIN=/path/to/codex`
- `CODEX_GO_INTEGRATION_MODEL=gpt-5.2`
- `CODEX_GO_INTEGRATION_EFFORT=xhigh`
- `CODEX_GO_INTEGRATION_TIMEOUT=2m`

The checked-in generated files are:

- [`codex/protocol/generated.go`](./codex/protocol/generated.go)
- [`codex/protocol/registry_generated.go`](./codex/protocol/registry_generated.go)

`contract_generation_test.go` reruns the generator and fails when those files drift from the current upstream schema inputs.

## Versioning notes

- This SDK is currently experimental and may change as the Go surface settles.
- The schema-derived types are intended to track the upstream app-server protocol schema from `openai/codex`.
- Keeping the Go SDK reasonably current with the Codex CLI version in use is recommended.
