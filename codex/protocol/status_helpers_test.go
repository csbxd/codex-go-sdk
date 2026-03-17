package protocol

import "testing"

func TestParseThreadStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      ThreadStatus
		want     ThreadStatusState
		wantErr  bool
		hasFlag  *ThreadActiveFlag
		isLoaded bool
		isActive bool
	}{
		{
			name:     "not loaded",
			raw:      ThreadStatus(`{"type":"notLoaded"}`),
			want:     ThreadStatusState{Kind: ThreadStatusKindNotLoaded},
			isLoaded: false,
			isActive: false,
		},
		{
			name:     "idle",
			raw:      ThreadStatus(`{"type":"idle"}`),
			want:     ThreadStatusState{Kind: ThreadStatusKindIdle},
			isLoaded: true,
			isActive: false,
		},
		{
			name:     "system error",
			raw:      ThreadStatus(`{"type":"systemError"}`),
			want:     ThreadStatusState{Kind: ThreadStatusKindSystemError},
			isLoaded: true,
			isActive: false,
		},
		{
			name: "active",
			raw: ThreadStatus(`{
				"type":"active",
				"activeFlags":["waitingOnApproval","waitingOnUserInput"]
			}`),
			want: ThreadStatusState{
				Kind: ThreadStatusKindActive,
				ActiveFlags: []ThreadActiveFlag{
					ThreadActiveFlagWaitingOnApproval,
					ThreadActiveFlagWaitingOnUserInput,
				},
			},
			hasFlag:  ptrThreadActiveFlag(ThreadActiveFlagWaitingOnApproval),
			isLoaded: true,
			isActive: true,
		},
		{
			name:    "empty",
			raw:     nil,
			wantErr: true,
		},
		{
			name:    "unknown kind",
			raw:     ThreadStatus(`{"type":"mystery"}`),
			wantErr: true,
		},
		{
			name:    "active flags on idle are rejected",
			raw:     ThreadStatus(`{"type":"idle","activeFlags":["waitingOnApproval"]}`),
			wantErr: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseThreadStatus(test.raw)
			if test.wantErr {
				if err == nil {
					t.Fatal("ParseThreadStatus() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseThreadStatus() error = %v", err)
			}

			if got.Kind != test.want.Kind {
				t.Fatalf("ParseThreadStatus() kind = %q, want %q", got.Kind, test.want.Kind)
			}
			if len(got.ActiveFlags) != len(test.want.ActiveFlags) {
				t.Fatalf("ParseThreadStatus() activeFlags = %#v, want %#v", got.ActiveFlags, test.want.ActiveFlags)
			}
			for index := range got.ActiveFlags {
				if got.ActiveFlags[index] != test.want.ActiveFlags[index] {
					t.Fatalf("ParseThreadStatus() activeFlags = %#v, want %#v", got.ActiveFlags, test.want.ActiveFlags)
				}
			}
			if got.IsLoaded() != test.isLoaded {
				t.Fatalf("ParseThreadStatus() IsLoaded() = %v, want %v", got.IsLoaded(), test.isLoaded)
			}
			if got.IsActive() != test.isActive {
				t.Fatalf("ParseThreadStatus() IsActive() = %v, want %v", got.IsActive(), test.isActive)
			}
			if test.hasFlag != nil && !got.HasActiveFlag(*test.hasFlag) {
				t.Fatalf("ParseThreadStatus() missing active flag %q", *test.hasFlag)
			}
		})
	}
}

func TestThreadStatusStateHelpers(t *testing.T) {
	t.Parallel()

	thread := Thread{
		Status: ThreadStatus(`{"type":"active","activeFlags":["waitingOnApproval"]}`),
	}
	state, err := thread.StatusState()
	if err != nil {
		t.Fatalf("Thread.StatusState() error = %v", err)
	}
	if !state.IsActive() || !state.HasActiveFlag(ThreadActiveFlagWaitingOnApproval) {
		t.Fatalf("Thread.StatusState() = %#v, want active waitingOnApproval", state)
	}

	notification := ThreadStatusChangedNotification{
		Status: ThreadStatus(`{"type":"idle"}`),
	}
	state, err = notification.StatusState()
	if err != nil {
		t.Fatalf("ThreadStatusChangedNotification.StatusState() error = %v", err)
	}
	if state.Kind != ThreadStatusKindIdle {
		t.Fatalf("ThreadStatusChangedNotification.StatusState() = %#v, want idle", state)
	}
}

func TestTurnStatusStateMachine(t *testing.T) {
	t.Parallel()

	if TurnStatusInProgress.IsTerminal() {
		t.Fatal("TurnStatusInProgress.IsTerminal() = true, want false")
	}
	for _, next := range []TurnStatus{
		TurnStatusCompleted,
		TurnStatusFailed,
		TurnStatusInterrupted,
	} {
		if !TurnStatusInProgress.CanTransitionTo(next) {
			t.Fatalf("TurnStatusInProgress.CanTransitionTo(%q) = false, want true", next)
		}
		if !next.IsTerminal() {
			t.Fatalf("%q IsTerminal() = false, want true", next)
		}
		if next.CanTransitionTo(TurnStatusInProgress) {
			t.Fatalf("%q.CanTransitionTo(inProgress) = true, want false", next)
		}
	}
}

func TestCommandExecutionStatusStateMachine(t *testing.T) {
	t.Parallel()

	if CommandExecutionStatusInProgress.IsTerminal() {
		t.Fatal("CommandExecutionStatusInProgress.IsTerminal() = true, want false")
	}
	for _, next := range []CommandExecutionStatus{
		CommandExecutionStatusCompleted,
		CommandExecutionStatusFailed,
		CommandExecutionStatusDeclined,
	} {
		if !CommandExecutionStatusInProgress.CanTransitionTo(next) {
			t.Fatalf("CommandExecutionStatusInProgress.CanTransitionTo(%q) = false, want true", next)
		}
		if !next.IsTerminal() {
			t.Fatalf("%q IsTerminal() = false, want true", next)
		}
		if next.CanTransitionTo(CommandExecutionStatusInProgress) {
			t.Fatalf("%q.CanTransitionTo(inProgress) = true, want false", next)
		}
	}
}

func TestPatchApplyStatusStateMachine(t *testing.T) {
	t.Parallel()

	if PatchApplyStatusInProgress.IsTerminal() {
		t.Fatal("PatchApplyStatusInProgress.IsTerminal() = true, want false")
	}
	for _, next := range []PatchApplyStatus{
		PatchApplyStatusCompleted,
		PatchApplyStatusFailed,
		PatchApplyStatusDeclined,
	} {
		if !PatchApplyStatusInProgress.CanTransitionTo(next) {
			t.Fatalf("PatchApplyStatusInProgress.CanTransitionTo(%q) = false, want true", next)
		}
		if !next.IsTerminal() {
			t.Fatalf("%q IsTerminal() = false, want true", next)
		}
	}
}

func TestToolCallStatusStateMachine(t *testing.T) {
	t.Parallel()

	if !McpToolCallStatusInProgress.CanTransitionTo(McpToolCallStatusCompleted) {
		t.Fatal("McpToolCallStatusInProgress cannot transition to completed")
	}
	if McpToolCallStatusCompleted.CanTransitionTo(McpToolCallStatusInProgress) {
		t.Fatal("McpToolCallStatusCompleted can transition back to inProgress")
	}

	if !DynamicToolCallStatusInProgress.CanTransitionTo(DynamicToolCallStatusFailed) {
		t.Fatal("DynamicToolCallStatusInProgress cannot transition to failed")
	}
	if DynamicToolCallStatusFailed.CanTransitionTo(DynamicToolCallStatusInProgress) {
		t.Fatal("DynamicToolCallStatusFailed can transition back to inProgress")
	}

	if !CollabAgentToolCallStatusInProgress.CanTransitionTo(CollabAgentToolCallStatusCompleted) {
		t.Fatal("CollabAgentToolCallStatusInProgress cannot transition to completed")
	}
	if CollabAgentToolCallStatusFailed.CanTransitionTo(CollabAgentToolCallStatusInProgress) {
		t.Fatal("CollabAgentToolCallStatusFailed can transition back to inProgress")
	}
}

func TestHookRunStatusStateMachine(t *testing.T) {
	t.Parallel()

	if HookRunStatusRunning.IsTerminal() {
		t.Fatal("HookRunStatusRunning.IsTerminal() = true, want false")
	}
	for _, next := range []HookRunStatus{
		HookRunStatusCompleted,
		HookRunStatusFailed,
		HookRunStatusBlocked,
		HookRunStatusStopped,
	} {
		if !HookRunStatusRunning.CanTransitionTo(next) {
			t.Fatalf("HookRunStatusRunning.CanTransitionTo(%q) = false, want true", next)
		}
		if !next.IsTerminal() {
			t.Fatalf("%q IsTerminal() = false, want true", next)
		}
	}
}

func TestCollabAgentStatusStateMachine(t *testing.T) {
	t.Parallel()

	if CollabAgentStatusPendingInit.IsTerminal() {
		t.Fatal("CollabAgentStatusPendingInit.IsTerminal() = true, want false")
	}
	if !CollabAgentStatusPendingInit.CanTransitionTo(CollabAgentStatusRunning) {
		t.Fatal("CollabAgentStatusPendingInit cannot transition to running")
	}
	for _, next := range []CollabAgentStatus{
		CollabAgentStatusCompleted,
		CollabAgentStatusErrored,
		CollabAgentStatusShutdown,
		CollabAgentStatusNotFound,
	} {
		if !CollabAgentStatusRunning.CanTransitionTo(next) {
			t.Fatalf("CollabAgentStatusRunning.CanTransitionTo(%q) = false, want true", next)
		}
		if !next.IsTerminal() {
			t.Fatalf("%q IsTerminal() = false, want true", next)
		}
		if next.CanTransitionTo(CollabAgentStatusRunning) {
			t.Fatalf("%q.CanTransitionTo(running) = true, want false", next)
		}
	}
}

func TestTurnPlanStepStatusStateMachine(t *testing.T) {
	t.Parallel()

	if TurnPlanStepStatusPending.IsTerminal() {
		t.Fatal("TurnPlanStepStatusPending.IsTerminal() = true, want false")
	}
	if !TurnPlanStepStatusPending.CanTransitionTo(TurnPlanStepStatusInProgress) {
		t.Fatal("TurnPlanStepStatusPending cannot transition to inProgress")
	}
	if !TurnPlanStepStatusPending.CanTransitionTo(TurnPlanStepStatusCompleted) {
		t.Fatal("TurnPlanStepStatusPending cannot transition to completed")
	}
	if !TurnPlanStepStatusInProgress.CanTransitionTo(TurnPlanStepStatusCompleted) {
		t.Fatal("TurnPlanStepStatusInProgress cannot transition to completed")
	}
	if TurnPlanStepStatusCompleted.CanTransitionTo(TurnPlanStepStatusInProgress) {
		t.Fatal("TurnPlanStepStatusCompleted can transition back to inProgress")
	}
}

func ptrThreadActiveFlag(v ThreadActiveFlag) *ThreadActiveFlag {
	return &v
}
