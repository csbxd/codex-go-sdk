package protocol

import (
	"fmt"
	"slices"
)

// ThreadStatusKind is a backward-compatible alias for ThreadStatusType.
type ThreadStatusKind = ThreadStatusType

const (
	ThreadStatusKindNotLoaded   ThreadStatusKind = ThreadStatusTypeNotLoaded
	ThreadStatusKindIdle        ThreadStatusKind = ThreadStatusTypeIdle
	ThreadStatusKindSystemError ThreadStatusKind = ThreadStatusTypeSystemError
	ThreadStatusKindActive      ThreadStatusKind = ThreadStatusTypeActive
)

// ThreadStatusState is a validated view of the generated ThreadStatus payload.
type ThreadStatusState struct {
	Kind        ThreadStatusKind
	ActiveFlags []ThreadActiveFlag
}

// ParseThreadStatus validates ThreadStatus and returns a stable helper view.
func ParseThreadStatus(status ThreadStatus) (ThreadStatusState, error) {
	if !status.Type.IsValid() {
		if status.Type == "" {
			return ThreadStatusState{}, fmt.Errorf("thread status type is empty")
		}
		return ThreadStatusState{}, fmt.Errorf("unknown thread status type %q", status.Type)
	}

	if status.Type != ThreadStatusTypeActive && len(status.ActiveFlags) > 0 {
		return ThreadStatusState{}, fmt.Errorf("thread status %q does not allow activeFlags", status.Type)
	}
	for _, flag := range status.ActiveFlags {
		if !flag.IsValid() {
			return ThreadStatusState{}, fmt.Errorf("unknown thread active flag %q", flag)
		}
	}
	return ThreadStatusState{
		Kind:        status.Type,
		ActiveFlags: append([]ThreadActiveFlag(nil), status.ActiveFlags...),
	}, nil
}

// MustParseThreadStatus decodes ThreadStatus and panics on invalid input.
func MustParseThreadStatus(raw ThreadStatus) ThreadStatusState {
	state, err := ParseThreadStatus(raw)
	if err != nil {
		panic(err)
	}
	return state
}

// IsLoaded reports whether the thread is currently loaded in the runtime.
func (s ThreadStatusState) IsLoaded() bool {
	return s.Kind != ThreadStatusKindNotLoaded
}

// IsActive reports whether the thread is currently active.
func (s ThreadStatusState) IsActive() bool {
	return s.Kind == ThreadStatusKindActive
}

// HasActiveFlag reports whether the active thread state contains the flag.
func (s ThreadStatusState) HasActiveFlag(flag ThreadActiveFlag) bool {
	return slices.Contains(s.ActiveFlags, flag)
}

// StatusState decodes Thread.Status.
func (t Thread) StatusState() (ThreadStatusState, error) {
	return ParseThreadStatus(t.Status)
}

// StatusState decodes ThreadStatusChangedNotification.Status.
func (n ThreadStatusChangedNotification) StatusState() (ThreadStatusState, error) {
	return ParseThreadStatus(n.Status)
}

// IsTerminal reports whether the turn status is terminal.
func (s TurnStatus) IsTerminal() bool {
	return s != TurnStatusInProgress
}

// CanTransitionTo reports whether a turn may move to next.
func (s TurnStatus) CanTransitionTo(next TurnStatus) bool {
	if s == next {
		return true
	}
	switch s {
	case TurnStatusInProgress:
		return next == TurnStatusCompleted || next == TurnStatusFailed || next == TurnStatusInterrupted
	case TurnStatusCompleted, TurnStatusFailed, TurnStatusInterrupted:
		return false
	default:
		return false
	}
}

// IsTerminal reports whether the command execution status is terminal.
func (s CommandExecutionStatus) IsTerminal() bool {
	return s != CommandExecutionStatusInProgress
}

// CanTransitionTo reports whether a command execution item may move to next.
func (s CommandExecutionStatus) CanTransitionTo(next CommandExecutionStatus) bool {
	if s == next {
		return true
	}
	switch s {
	case CommandExecutionStatusInProgress:
		return next == CommandExecutionStatusCompleted ||
			next == CommandExecutionStatusFailed ||
			next == CommandExecutionStatusDeclined
	case CommandExecutionStatusCompleted, CommandExecutionStatusFailed, CommandExecutionStatusDeclined:
		return false
	default:
		return false
	}
}

// IsTerminal reports whether the patch status is terminal.
func (s PatchApplyStatus) IsTerminal() bool {
	return s != PatchApplyStatusInProgress
}

// CanTransitionTo reports whether a file-change item may move to next.
func (s PatchApplyStatus) CanTransitionTo(next PatchApplyStatus) bool {
	if s == next {
		return true
	}
	switch s {
	case PatchApplyStatusInProgress:
		return next == PatchApplyStatusCompleted ||
			next == PatchApplyStatusFailed ||
			next == PatchApplyStatusDeclined
	case PatchApplyStatusCompleted, PatchApplyStatusFailed, PatchApplyStatusDeclined:
		return false
	default:
		return false
	}
}

// IsTerminal reports whether the MCP tool call status is terminal.
func (s McpToolCallStatus) IsTerminal() bool {
	return s != McpToolCallStatusInProgress
}

// CanTransitionTo reports whether an MCP tool call may move to next.
func (s McpToolCallStatus) CanTransitionTo(next McpToolCallStatus) bool {
	if s == next {
		return true
	}
	switch s {
	case McpToolCallStatusInProgress:
		return next == McpToolCallStatusCompleted || next == McpToolCallStatusFailed
	case McpToolCallStatusCompleted, McpToolCallStatusFailed:
		return false
	default:
		return false
	}
}

// IsTerminal reports whether the dynamic tool call status is terminal.
func (s DynamicToolCallStatus) IsTerminal() bool {
	return s != DynamicToolCallStatusInProgress
}

// CanTransitionTo reports whether a dynamic tool call may move to next.
func (s DynamicToolCallStatus) CanTransitionTo(next DynamicToolCallStatus) bool {
	if s == next {
		return true
	}
	switch s {
	case DynamicToolCallStatusInProgress:
		return next == DynamicToolCallStatusCompleted || next == DynamicToolCallStatusFailed
	case DynamicToolCallStatusCompleted, DynamicToolCallStatusFailed:
		return false
	default:
		return false
	}
}

// IsTerminal reports whether the collab tool call status is terminal.
func (s CollabAgentToolCallStatus) IsTerminal() bool {
	return s != CollabAgentToolCallStatusInProgress
}

// CanTransitionTo reports whether a collab tool call may move to next.
func (s CollabAgentToolCallStatus) CanTransitionTo(next CollabAgentToolCallStatus) bool {
	if s == next {
		return true
	}
	switch s {
	case CollabAgentToolCallStatusInProgress:
		return next == CollabAgentToolCallStatusCompleted || next == CollabAgentToolCallStatusFailed
	case CollabAgentToolCallStatusCompleted, CollabAgentToolCallStatusFailed:
		return false
	default:
		return false
	}
}

// IsTerminal reports whether the hook run status is terminal.
func (s HookRunStatus) IsTerminal() bool {
	return s != HookRunStatusRunning
}

// CanTransitionTo reports whether a hook run may move to next.
func (s HookRunStatus) CanTransitionTo(next HookRunStatus) bool {
	if s == next {
		return true
	}
	switch s {
	case HookRunStatusRunning:
		return next == HookRunStatusCompleted ||
			next == HookRunStatusFailed ||
			next == HookRunStatusBlocked ||
			next == HookRunStatusStopped
	case HookRunStatusCompleted, HookRunStatusFailed, HookRunStatusBlocked, HookRunStatusStopped:
		return false
	default:
		return false
	}
}

// IsTerminal reports whether the collab agent status is terminal.
func (s CollabAgentStatus) IsTerminal() bool {
	return s != CollabAgentStatusPendingInit && s != CollabAgentStatusRunning
}

// CanTransitionTo reports whether a collab agent may move to next.
func (s CollabAgentStatus) CanTransitionTo(next CollabAgentStatus) bool {
	if s == next {
		return true
	}
	switch s {
	case CollabAgentStatusPendingInit:
		return next == CollabAgentStatusRunning || next.IsTerminal()
	case CollabAgentStatusRunning:
		return next.IsTerminal()
	case CollabAgentStatusCompleted, CollabAgentStatusErrored, CollabAgentStatusShutdown, CollabAgentStatusNotFound:
		return false
	default:
		return false
	}
}

// IsTerminal reports whether the plan step status is terminal.
func (s TurnPlanStepStatus) IsTerminal() bool {
	return s == TurnPlanStepStatusCompleted
}

// CanTransitionTo reports whether a plan step may move to next.
func (s TurnPlanStepStatus) CanTransitionTo(next TurnPlanStepStatus) bool {
	if s == next {
		return true
	}
	switch s {
	case TurnPlanStepStatusPending:
		return next == TurnPlanStepStatusInProgress || next == TurnPlanStepStatusCompleted
	case TurnPlanStepStatusInProgress:
		return next == TurnPlanStepStatusCompleted
	case TurnPlanStepStatusCompleted:
		return false
	default:
		return false
	}
}
