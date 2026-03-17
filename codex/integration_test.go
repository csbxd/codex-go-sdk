package codexappserver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/csbxd/codex-go-sdk/codex/protocol"
)

const (
	integrationEnabledEnv  = "CODEX_GO_INTEGRATION"
	integrationCodexBinEnv = "CODEX_GO_INTEGRATION_CODEX_BIN"
	integrationModelEnv    = "CODEX_GO_INTEGRATION_MODEL"
	integrationEffortEnv   = "CODEX_GO_INTEGRATION_EFFORT"
	integrationTimeoutEnv  = "CODEX_GO_INTEGRATION_TIMEOUT"

	preferredIntegrationModel = "gpt-5.2"
	defaultIntegrationTimeout = 2 * time.Minute
)

type integrationConfig struct {
	codexBin string
	timeout  time.Duration
}

func TestIntegrationAppServerCoreFlow(t *testing.T) {
	cfg := requireIntegrationConfig(t)
	workDir := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	client := NewClient(Config{
		CodexBin: cfg.codexBin,
		CWD:      workDir,
	})
	defer func() {
		_ = client.Close()
	}()

	initResp, err := client.Open(ctx)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if initResp.UserAgent == "" {
		t.Fatal("Open() userAgent = empty, want non-empty")
	}

	model := selectIntegrationModel(t, ctx, client)
	effort := chooseIntegrationEffort(t, model)

	started, err := client.ThreadStart(ctx, &protocol.ThreadStartParams{
		CWD:       Ptr(workDir),
		Ephemeral: Ptr(true),
		Model:     Ptr(model.ID),
		Sandbox:   Ptr(protocol.SandboxModeWorkspaceWrite),
	})
	if err != nil {
		t.Fatalf("ThreadStart() error = %v", err)
	}

	turnStarted, err := client.TurnStartText(
		ctx,
		started.Thread.ID,
		"Reply with exactly OK.",
		&protocol.TurnStartParams{
			CWD:    Ptr(workDir),
			Effort: Ptr(effort),
		},
	)
	if err != nil {
		t.Fatalf("TurnStartText() error = %v", err)
	}

	completed, err := client.WaitForTurnCompleted(ctx, turnStarted.Turn.ID)
	if err != nil {
		t.Fatalf("WaitForTurnCompleted() error = %v", err)
	}
	if completed.Turn.Status != protocol.TurnStatusCompleted {
		t.Fatalf("WaitForTurnCompleted() status = %q, want %q", completed.Turn.Status, protocol.TurnStatusCompleted)
	}

	read, err := client.ThreadRead(ctx, &protocol.ThreadReadParams{
		ThreadId: started.Thread.ID,
	})
	if err != nil {
		t.Fatalf("ThreadRead() error = %v", err)
	}
	if read.Thread.ID != started.Thread.ID {
		t.Fatalf("ThreadRead() thread id = %q, want %q", read.Thread.ID, started.Thread.ID)
	}
	if read.Thread.CWD != workDir {
		t.Fatalf("ThreadRead() cwd = %q, want %q", read.Thread.CWD, workDir)
	}
	if !read.Thread.Ephemeral {
		t.Fatal("ThreadRead() ephemeral = false, want true")
	}
}

func TestIntegrationFileChangeFlow(t *testing.T) {
	cfg := requireIntegrationConfig(t)
	workDir := t.TempDir()
	approveCh := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	requestCh := make(chan ServerRequest, 1)
	client := NewClient(Config{
		CodexBin: cfg.codexBin,
		CWD:      workDir,
		RequestHandler: func(ctx context.Context, request ServerRequest) (any, error) {
			requestCh <- request
			select {
			case <-approveCh:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return map[string]any{"decision": "accept"}, nil
		},
	})
	defer func() {
		_ = client.Close()
	}()

	if _, err := client.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	model := selectIntegrationModel(t, ctx, client)
	effort := chooseApprovalEffort(t, model)
	approvalPolicy := protocol.AskForApproval([]byte(`"untrusted"`))

	started, err := client.ThreadStart(ctx, &protocol.ThreadStartParams{
		ApprovalPolicy: &approvalPolicy,
		CWD:            Ptr(workDir),
		Ephemeral:      Ptr(true),
		Model:          Ptr(model.ID),
		Sandbox:        Ptr(protocol.SandboxModeWorkspaceWrite),
	})
	if err != nil {
		t.Fatalf("ThreadStart() error = %v", err)
	}

	turnStarted, err := client.TurnStartText(
		ctx,
		started.Thread.ID,
		"Use the apply_patch tool, not shell commands, to apply this exact patch:\n*** Begin Patch\n*** Add File: approval_probe.txt\n+hello\n*** End Patch",
		&protocol.TurnStartParams{
			ApprovalPolicy: &approvalPolicy,
			CWD:            Ptr(workDir),
			Effort:         Ptr(effort),
		},
	)
	if err != nil {
		t.Fatalf("TurnStartText() error = %v", err)
	}

	var request ServerRequest
	select {
	case request = <-requestCh:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for approval request: %v", ctx.Err())
	}

	if request.Method != "item/fileChange/requestApproval" {
		t.Fatalf("request.Method = %q, want %q", request.Method, "item/fileChange/requestApproval")
	}
	params, ok := request.Payload.(*protocol.FileChangeRequestApprovalParams)
	if !ok {
		t.Fatalf("request.Payload type = %T, want *protocol.FileChangeRequestApprovalParams", request.Payload)
	}
	if params.ThreadId != started.Thread.ID {
		t.Fatalf("request thread id = %q, want %q", params.ThreadId, started.Thread.ID)
	}
	if params.TurnId != turnStarted.Turn.ID {
		t.Fatalf("request turn id = %q, want %q", params.TurnId, turnStarted.Turn.ID)
	}

	if _, err := os.Stat(filepath.Join(workDir, "approval_probe.txt")); !os.IsNotExist(err) {
		t.Fatalf("approval_probe.txt exists before approval, stat error = %v", err)
	}

	close(approveCh)

	completed, err := client.WaitForTurnCompleted(ctx, turnStarted.Turn.ID)
	if err != nil {
		t.Fatalf("WaitForTurnCompleted() error = %v", err)
	}
	if completed.Turn.Status != protocol.TurnStatusCompleted {
		t.Fatalf("WaitForTurnCompleted() status = %q, want %q", completed.Turn.Status, protocol.TurnStatusCompleted)
	}

	content, err := os.ReadFile(filepath.Join(workDir, "approval_probe.txt"))
	if err != nil {
		t.Fatalf("ReadFile(approval_probe.txt) error = %v", err)
	}
	if strings.TrimSpace(string(content)) != "hello" {
		t.Fatalf("approval_probe.txt = %q, want hello", string(content))
	}
}

func requireIntegrationConfig(t *testing.T) integrationConfig {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	if os.Getenv(integrationEnabledEnv) == "" {
		t.Skipf("set %s=1 to run integration tests", integrationEnabledEnv)
	}

	timeout := defaultIntegrationTimeout
	if raw := os.Getenv(integrationTimeoutEnv); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			t.Fatalf("invalid %s value %q: %v", integrationTimeoutEnv, raw, err)
		}
		timeout = parsed
	}

	codexBin := os.Getenv(integrationCodexBinEnv)
	if codexBin == "" {
		resolved, err := exec.LookPath("codex")
		if err != nil {
			t.Skipf("codex binary not found on PATH: %v", err)
		}
		codexBin = resolved
	}

	return integrationConfig{
		codexBin: codexBin,
		timeout:  timeout,
	}
}

func selectIntegrationModel(
	t *testing.T,
	ctx context.Context,
	client *Client,
) protocol.Model {
	t.Helper()

	models, err := client.ModelList(ctx, &protocol.ModelListParams{
		IncludeHidden: Ptr(true),
	})
	if err != nil {
		t.Fatalf("ModelList() error = %v", err)
	}
	if len(models.Data) == 0 {
		t.Fatal("ModelList() returned no models")
	}

	return chooseIntegrationModel(t, models.Data)
}

func chooseIntegrationModel(t *testing.T, models []protocol.Model) protocol.Model {
	t.Helper()

	requested := os.Getenv(integrationModelEnv)
	if requested != "" {
		for _, model := range models {
			if model.ID == requested {
				return model
			}
		}
		t.Fatalf("%s=%q is not available", integrationModelEnv, requested)
	}

	for _, model := range models {
		if model.ID == preferredIntegrationModel {
			return model
		}
	}
	for _, model := range models {
		if model.IsDefault {
			return model
		}
	}
	return models[0]
}

func chooseIntegrationEffort(t *testing.T, model protocol.Model) protocol.ReasoningEffort {
	t.Helper()

	requested := os.Getenv(integrationEffortEnv)
	if requested != "" {
		effort := protocol.ReasoningEffort(requested)
		if !supportsReasoningEffort(model, effort) {
			t.Fatalf("%s=%q is not supported by model %q", integrationEffortEnv, requested, model.ID)
		}
		return effort
	}

	if supportsReasoningEffort(model, protocol.ReasoningEffortXhigh) {
		return protocol.ReasoningEffortXhigh
	}
	if model.DefaultReasoningEffort != "" {
		return model.DefaultReasoningEffort
	}
	return protocol.ReasoningEffortMedium
}

func chooseApprovalEffort(t *testing.T, model protocol.Model) protocol.ReasoningEffort {
	t.Helper()

	if os.Getenv(integrationEffortEnv) != "" {
		return chooseIntegrationEffort(t, model)
	}
	if supportsReasoningEffort(model, protocol.ReasoningEffortMedium) {
		return protocol.ReasoningEffortMedium
	}
	return chooseIntegrationEffort(t, model)
}

func supportsReasoningEffort(model protocol.Model, effort protocol.ReasoningEffort) bool {
	for _, option := range model.SupportedReasoningEfforts {
		if option.ReasoningEffort == effort {
			return true
		}
	}
	return false
}
