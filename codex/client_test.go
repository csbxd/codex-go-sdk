package codexappserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/csbxd/codex-go-sdk/codex/protocol"
)

func TestOpenAndCoreRPCMethods(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "core.jsonl")
	client := NewClient(Config{
		LaunchArgsOverride: helperArgs(),
		Env: helperEnv(map[string]string{
			"CODEX_GO_TEST_SCENARIO": "core",
			"CODEX_GO_TEST_LOG":      logPath,
		}),
	})
	defer func() {
		_ = client.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	initResp, err := client.Open(ctx)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if initResp.UserAgent != "codex/test" {
		t.Fatalf("Open() user agent = %q, want %q", initResp.UserAgent, "codex/test")
	}

	started, err := client.ThreadStart(ctx, &protocol.ThreadStartParams{
		Model: Ptr("gpt-5"),
	})
	if err != nil {
		t.Fatalf("ThreadStart() error = %v", err)
	}
	if started.Thread.ID != "thread-1" {
		t.Fatalf("ThreadStart() thread id = %q, want %q", started.Thread.ID, "thread-1")
	}

	listed, err := client.ThreadList(ctx, &protocol.ThreadListParams{
		SearchTerm: Ptr("needle"),
		Limit:      Ptr[uint32](5),
	})
	if err != nil {
		t.Fatalf("ThreadList() error = %v", err)
	}
	if len(listed.Data) != 1 || listed.Data[0].ID != "thread-1" {
		t.Fatalf("ThreadList() = %#v, want one thread", listed)
	}

	read, err := client.ThreadRead(ctx, &protocol.ThreadReadParams{
		ThreadId:     "thread-1",
		IncludeTurns: Ptr(true),
	})
	if err != nil {
		t.Fatalf("ThreadRead() error = %v", err)
	}
	if read.Thread.ID != "thread-1" {
		t.Fatalf("ThreadRead() thread id = %q, want %q", read.Thread.ID, "thread-1")
	}

	if _, err := client.ThreadSetName(ctx, &protocol.ThreadSetNameParams{
		ThreadId: "thread-1",
		Name:     "sdk-name",
	}); err != nil {
		t.Fatalf("ThreadSetName() error = %v", err)
	}
	if _, err := client.ThreadCompact(ctx, &protocol.ThreadCompactStartParams{
		ThreadId: "thread-1",
	}); err != nil {
		t.Fatalf("ThreadCompact() error = %v", err)
	}
	if _, err := client.ThreadArchive(ctx, &protocol.ThreadArchiveParams{
		ThreadId: "thread-1",
	}); err != nil {
		t.Fatalf("ThreadArchive() error = %v", err)
	}
	if _, err := client.ThreadUnarchive(ctx, &protocol.ThreadUnarchiveParams{
		ThreadId: "thread-1",
	}); err != nil {
		t.Fatalf("ThreadUnarchive() error = %v", err)
	}
	if _, err := client.ThreadFork(ctx, &protocol.ThreadForkParams{
		ThreadId: "thread-1",
	}); err != nil {
		t.Fatalf("ThreadFork() error = %v", err)
	}
	if _, err := client.ThreadResume(ctx, &protocol.ThreadResumeParams{
		ThreadId: "thread-1",
	}); err != nil {
		t.Fatalf("ThreadResume() error = %v", err)
	}

	turnStarted, err := client.TurnStartText(ctx, "thread-1", "Say hello in one sentence.", nil)
	if err != nil {
		t.Fatalf("TurnStartText() error = %v", err)
	}
	if turnStarted.Turn.ID != "turn-1" {
		t.Fatalf("TurnStartText() turn id = %q, want %q", turnStarted.Turn.ID, "turn-1")
	}

	notification, err := client.NextNotification(ctx)
	if err != nil {
		t.Fatalf("NextNotification() error = %v", err)
	}
	if notification.Method != "item/agentMessage/delta" {
		t.Fatalf("NextNotification() method = %q, want %q", notification.Method, "item/agentMessage/delta")
	}
	if _, ok := notification.Payload.(*protocol.AgentMessageDeltaNotification); !ok {
		t.Fatalf("NextNotification() payload type = %T, want *protocol.AgentMessageDeltaNotification", notification.Payload)
	}

	notification, err = client.NextNotification(ctx)
	if err != nil {
		t.Fatalf("NextNotification() second error = %v", err)
	}
	if notification.Method != "thread/tokenUsage/updated" {
		t.Fatalf("NextNotification() second method = %q, want %q", notification.Method, "thread/tokenUsage/updated")
	}
	if _, ok := notification.Payload.(*protocol.ThreadTokenUsageUpdatedNotification); !ok {
		t.Fatalf("NextNotification() second payload type = %T, want *protocol.ThreadTokenUsageUpdatedNotification", notification.Payload)
	}

	completed, err := client.WaitForTurnCompleted(ctx, "turn-1")
	if err != nil {
		t.Fatalf("WaitForTurnCompleted() error = %v", err)
	}
	if completed.Turn.Status != protocol.TurnStatusCompleted {
		t.Fatalf("WaitForTurnCompleted() status = %q, want %q", completed.Turn.Status, protocol.TurnStatusCompleted)
	}

	if _, err := client.TurnSteerText(ctx, "thread-1", "turn-1", "Continue."); err != nil {
		t.Fatalf("TurnSteerText() error = %v", err)
	}
	if _, err := client.TurnInterrupt(ctx, &protocol.TurnInterruptParams{
		ThreadId: "thread-1",
		TurnId:   "turn-1",
	}); err != nil {
		t.Fatalf("TurnInterrupt() error = %v", err)
	}

	models, err := client.ModelList(ctx, &protocol.ModelListParams{
		IncludeHidden: Ptr(true),
	})
	if err != nil {
		t.Fatalf("ModelList() error = %v", err)
	}
	if len(models.Data) != 1 || models.Data[0].ID != "gpt-5" {
		t.Fatalf("ModelList() = %#v, want one model", models)
	}

	lines := readLogLines(t, logPath)
	methods := logMethods(t, lines)
	wantMethods := []string{
		"initialize",
		"initialized",
		"thread/start",
		"thread/list",
		"thread/read",
		"thread/name/set",
		"thread/compact/start",
		"thread/archive",
		"thread/unarchive",
		"thread/fork",
		"thread/resume",
		"turn/start",
		"turn/steer",
		"turn/interrupt",
		"model/list",
	}
	if strings.Join(methods, ",") != strings.Join(wantMethods, ",") {
		t.Fatalf("logged methods = %v, want %v", methods, wantMethods)
	}

	listParams := logParamsForMethod(t, lines, "thread/list")
	if got := listParams["searchTerm"]; got != "needle" {
		t.Fatalf("thread/list searchTerm = %#v, want %q", got, "needle")
	}
	if _, ok := listParams["search_term"]; ok {
		t.Fatalf("thread/list serialized snake_case params: %#v", listParams)
	}

	turnStartParams := logParamsForMethod(t, lines, "turn/start")
	if got := turnStartParams["threadId"]; got != "thread-1" {
		t.Fatalf("turn/start threadId = %#v, want %q", got, "thread-1")
	}
	input, ok := turnStartParams["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("turn/start input = %#v, want one input item", turnStartParams["input"])
	}
}

func TestUnknownNotificationFallsBackToUnknownPayload(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{
		LaunchArgsOverride: helperArgs(),
		Env: helperEnv(map[string]string{
			"CODEX_GO_TEST_SCENARIO": "unknown-notification",
		}),
	})
	defer func() {
		_ = client.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	notification, err := client.NextNotification(ctx)
	if err != nil {
		t.Fatalf("NextNotification() error = %v", err)
	}
	if notification.Method != "unknown/notification" {
		t.Fatalf("NextNotification() method = %q, want %q", notification.Method, "unknown/notification")
	}

	payload, ok := notification.Payload.(UnknownPayload)
	if !ok {
		t.Fatalf("NextNotification() payload type = %T, want UnknownPayload", notification.Payload)
	}
	msg, ok := payload.Params["msg"].(map[string]any)
	if !ok || msg["type"] != "turn_aborted" {
		t.Fatalf("UnknownPayload msg = %#v, want turn_aborted", payload.Params["msg"])
	}
}

func TestApprovalHandlerRespondsToServerRequest(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "approval.jsonl")
	requestCh := make(chan ServerRequest, 1)

	client := NewClient(Config{
		LaunchArgsOverride: helperArgs(),
		Env: helperEnv(map[string]string{
			"CODEX_GO_TEST_SCENARIO": "approval",
			"CODEX_GO_TEST_LOG":      logPath,
		}),
		RequestHandler: func(ctx context.Context, request ServerRequest) (any, error) {
			requestCh <- request
			return map[string]any{"decision": "accept"}, nil
		},
	})
	defer func() {
		_ = client.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	select {
	case request := <-requestCh:
		if request.Method != "item/commandExecution/requestApproval" {
			t.Fatalf("request.Method = %q, want %q", request.Method, "item/commandExecution/requestApproval")
		}
		if _, ok := request.Payload.(*protocol.CommandExecutionRequestApprovalParams); !ok {
			t.Fatalf("request.Payload type = %T, want *protocol.CommandExecutionRequestApprovalParams", request.Payload)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for approval request: %v", ctx.Err())
	}

	waitForLogEntry(t, logPath, func(entry map[string]any) bool {
		id, _ := entry["id"].(string)
		if id != "approval-1" {
			return false
		}
		result, ok := entry["result"].(map[string]any)
		return ok && result["decision"] == "accept"
	})
}

func TestTransportClosedIncludesStderrTail(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{
		LaunchArgsOverride: helperArgs(),
		Env: helperEnv(map[string]string{
			"CODEX_GO_TEST_SCENARIO": "transport-close",
		}),
	})
	defer func() {
		_ = client.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Open(ctx)
	if err == nil {
		t.Fatal("Open() error = nil, want TransportClosedError")
	}

	var transportErr *TransportClosedError
	if !errors.As(err, &transportErr) {
		t.Fatalf("Open() error = %T, want *TransportClosedError", err)
	}
	if !strings.Contains(transportErr.StderrTail, "helper stderr boom") {
		t.Fatalf("TransportClosedError stderr tail = %q, want helper stderr boom", transportErr.StderrTail)
	}
}

func TestRetryOnOverload(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	attempts := 0
	value, err := RetryOnOverload(ctx, 4, time.Millisecond, 2*time.Millisecond, func(context.Context) (string, error) {
		attempts++
		if attempts < 3 {
			return "", &JSONRPCError{
				Code:    -32001,
				Message: "Server overloaded; retry later.",
			}
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("RetryOnOverload() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("RetryOnOverload() attempts = %d, want %d", attempts, 3)
	}
	if value != "ok" {
		t.Fatalf("RetryOnOverload() value = %q, want %q", value, "ok")
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	scenario := os.Getenv("CODEX_GO_TEST_SCENARIO")
	logPath := os.Getenv("CODEX_GO_TEST_LOG")

	reader := bufio.NewReader(os.Stdin)
	writer := json.NewEncoder(os.Stdout)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}

		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		if logPath != "" {
			appendLogLine(t, logPath, line)
		}

		var message map[string]any
		if err := json.Unmarshal(line, &message); err != nil {
			panic(err)
		}

		method, _ := message["method"].(string)
		id := message["id"]

		switch method {
		case "initialize":
			if scenario == "transport-close" {
				_, _ = os.Stderr.WriteString("helper stderr boom\n")
				_ = os.Stdout.Sync()
				_ = os.Stderr.Sync()
				os.Exit(0)
			}
			mustEncode(writer, map[string]any{
				"id": id,
				"result": map[string]any{
					"userAgent": "codex/test",
				},
			})
		case "initialized":
			switch scenario {
			case "approval":
				mustEncode(writer, map[string]any{
					"id":     "approval-1",
					"method": "item/commandExecution/requestApproval",
					"params": map[string]any{
						"itemId":   "item-1",
						"threadId": "thread-1",
						"turnId":   "turn-1",
						"command":  "echo hi",
					},
				})
			case "unknown-notification":
				mustEncode(writer, map[string]any{
					"method": "unknown/notification",
					"params": map[string]any{
						"msg": map[string]any{
							"type": "turn_aborted",
						},
					},
				})
			}
		case "thread/start":
			mustEncode(writer, map[string]any{
				"id":     id,
				"result": sampleThreadStartResponse("thread-1"),
			})
		case "thread/list":
			mustEncode(writer, map[string]any{
				"id": id,
				"result": map[string]any{
					"data":       []any{sampleThread("thread-1")},
					"nextCursor": nil,
				},
			})
		case "thread/read":
			mustEncode(writer, map[string]any{
				"id": id,
				"result": map[string]any{
					"thread": sampleThread("thread-1"),
				},
			})
		case "thread/name/set", "thread/compact/start", "thread/archive", "turn/interrupt":
			mustEncode(writer, map[string]any{
				"id":     id,
				"result": map[string]any{},
			})
		case "thread/unarchive":
			mustEncode(writer, map[string]any{
				"id": id,
				"result": map[string]any{
					"thread": sampleThread("thread-1"),
				},
			})
		case "thread/fork", "thread/resume":
			mustEncode(writer, map[string]any{
				"id":     id,
				"result": sampleThreadStartResponse("thread-1"),
			})
		case "turn/start":
			mustEncode(writer, map[string]any{
				"id": id,
				"result": map[string]any{
					"turn": sampleTurn("turn-1", "inProgress"),
				},
			})
			if scenario == "core" {
				mustEncode(writer, map[string]any{
					"method": "item/agentMessage/delta",
					"params": map[string]any{
						"delta":    "hello",
						"itemId":   "item-1",
						"threadId": "thread-1",
						"turnId":   "turn-1",
					},
				})
				mustEncode(writer, map[string]any{
					"method": "thread/tokenUsage/updated",
					"params": map[string]any{
						"threadId": "thread-1",
						"turnId":   "turn-1",
						"tokenUsage": map[string]any{
							"last": map[string]any{
								"cachedInputTokens":     0,
								"inputTokens":           1,
								"outputTokens":          2,
								"reasoningOutputTokens": 0,
								"totalTokens":           3,
							},
							"total": map[string]any{
								"cachedInputTokens":     0,
								"inputTokens":           1,
								"outputTokens":          2,
								"reasoningOutputTokens": 0,
								"totalTokens":           3,
							},
						},
					},
				})
				mustEncode(writer, map[string]any{
					"method": "turn/completed",
					"params": map[string]any{
						"threadId": "thread-1",
						"turn":     sampleTurn("turn-1", "completed"),
					},
				})
			}
		case "turn/steer":
			mustEncode(writer, map[string]any{
				"id": id,
				"result": map[string]any{
					"turnId": "turn-1",
				},
			})
		case "model/list":
			mustEncode(writer, map[string]any{
				"id": id,
				"result": map[string]any{
					"data": []any{
						map[string]any{
							"defaultReasoningEffort": "medium",
							"description":            "Test model",
							"displayName":            "GPT-5",
							"hidden":                 false,
							"id":                     "gpt-5",
							"isDefault":              true,
							"model":                  "gpt-5",
							"supportedReasoningEfforts": []any{
								map[string]any{
									"description":     "Default",
									"reasoningEffort": "medium",
								},
							},
						},
					},
					"nextCursor": nil,
				},
			})
		default:
			if scenario == "transport-close" && method == "initialize" {
				continue
			}
			if scenario == "transport-close" && id != nil {
				continue
			}
		}

	}
}

func helperArgs() []string {
	return []string{os.Args[0], "-test.run=TestHelperProcess", "--"}
}

func helperEnv(extra map[string]string) map[string]string {
	env := map[string]string{
		"GO_WANT_HELPER_PROCESS": "1",
	}
	for key, value := range extra {
		env[key] = value
	}
	return env
}

func sampleThreadStartResponse(threadID string) map[string]any {
	return map[string]any{
		"approvalPolicy": "never",
		"cwd":            "/tmp/project",
		"model":          "gpt-5",
		"modelProvider":  "openai",
		"sandbox":        "workspace-write",
		"serviceTier":    nil,
		"thread":         sampleThread(threadID),
	}
}

func sampleThread(threadID string) map[string]any {
	return map[string]any{
		"cliVersion":    "0.0.0-test",
		"createdAt":     1,
		"cwd":           "/tmp/project",
		"ephemeral":     false,
		"id":            threadID,
		"modelProvider": "openai",
		"preview":       "Test thread",
		"source":        "appServer",
		"status":        "loaded",
		"turns":         []any{},
		"updatedAt":     1,
	}
}

func sampleTurn(turnID string, status string) map[string]any {
	return map[string]any{
		"id":     turnID,
		"items":  []any{},
		"status": status,
	}
}

func mustEncode(writer *json.Encoder, value any) {
	if err := writer.Encode(value); err != nil {
		panic(err)
	}
}

func appendLogLine(t *testing.T, logPath string, line []byte) {
	t.Helper()
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if _, err := f.Write(append(append([]byte(nil), line...), '\n')); err != nil {
		panic(err)
	}
}

func readLogLines(t *testing.T, logPath string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", logPath, err)
	}
	rawLines := bytes.Split(bytes.TrimSpace(data), []byte{'\n'})
	lines := make([][]byte, 0, len(rawLines))
	for _, line := range rawLines {
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			lines = append(lines, append([]byte(nil), line...))
		}
	}
	return lines
}

func logMethods(t *testing.T, lines [][]byte) []string {
	t.Helper()
	var methods []string
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("json.Unmarshal(log line) error = %v", err)
		}
		method, _ := entry["method"].(string)
		if method != "" {
			methods = append(methods, method)
		}
	}
	return methods
}

func logParamsForMethod(t *testing.T, lines [][]byte, method string) map[string]any {
	t.Helper()
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("json.Unmarshal(log line) error = %v", err)
		}
		if got, _ := entry["method"].(string); got == method {
			params, _ := entry["params"].(map[string]any)
			return params
		}
	}
	t.Fatalf("did not find method %q in log", method)
	return nil
}

func waitForLogEntry(t *testing.T, logPath string, match func(map[string]any) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(logPath); err == nil {
			lines := readLogLines(t, logPath)
			for _, line := range lines {
				var entry map[string]any
				if err := json.Unmarshal(line, &entry); err != nil {
					t.Fatalf("json.Unmarshal(log line) error = %v", err)
				}
				if match(entry) {
					return
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for matching log entry in %s", logPath)
}
