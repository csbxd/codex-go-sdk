package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/csbxd/codex-go-sdk/codex/protocol"
)

type recordingWriteCloser struct {
	writes  [][]byte
	writeFn func([]byte) error
}

func (r *recordingWriteCloser) Write(p []byte) (int, error) {
	clone := append([]byte(nil), p...)
	r.writes = append(r.writes, clone)
	if r.writeFn != nil {
		if err := r.writeFn(clone); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (r *recordingWriteCloser) Close() error {
	return nil
}

func TestMarshalUserInputHelpers(t *testing.T) {
	t.Parallel()

	stringPtr := func(v string) *string {
		return &v
	}

	input, err := MarshalUserInput(map[string]any{
		"type": "text",
		"text": "hello",
	})
	if err != nil {
		t.Fatalf("MarshalUserInput() error = %v", err)
	}

	want := protocol.UserInput{
		Type: protocol.UserInputTypeText,
		Text: stringPtr("hello"),
	}
	if !reflect.DeepEqual(input, want) {
		t.Fatalf("MarshalUserInput() = %#v, want %#v", input, want)
	}

	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal(input) error = %v", err)
	}
	if string(encoded) != `{"type":"text","text":"hello"}` {
		t.Fatalf("json.Marshal(input) = %s, want text payload", encoded)
	}

	if _, err := MarshalUserInput(make(chan int)); err == nil {
		t.Fatal("MarshalUserInput() error = nil, want error")
	}

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("MustUserInput() did not panic for unsupported input")
		}
	}()
	_ = MustUserInput(make(chan int))
}

func TestLaunchArgs(t *testing.T) {
	t.Run("override", func(t *testing.T) {
		client := NewClient(Config{
			LaunchArgsOverride: []string{"custom-codex", "app-server", "--listen", "stdio://"},
		})

		args, err := client.launchArgs()
		if err != nil {
			t.Fatalf("launchArgs() error = %v", err)
		}
		if got := strings.Join(args, " "); got != "custom-codex app-server --listen stdio://" {
			t.Fatalf("launchArgs() = %q, want override args", got)
		}
	})

	t.Run("explicit binary with config overrides", func(t *testing.T) {
		client := NewClient(Config{
			CodexBin:        "/tmp/codex",
			ConfigOverrides: []string{"model=\"gpt-5.2\"", "approval_policy=\"never\""},
		})

		args, err := client.launchArgs()
		if err != nil {
			t.Fatalf("launchArgs() error = %v", err)
		}
		want := []string{
			"/tmp/codex",
			"--config", "model=\"gpt-5.2\"",
			"--config", "approval_policy=\"never\"",
			"app-server", "--listen", "stdio://",
		}
		if strings.Join(args, "\n") != strings.Join(want, "\n") {
			t.Fatalf("launchArgs() = %#v, want %#v", args, want)
		}
	})

	t.Run("missing codex on path", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())

		client := NewClient(Config{})
		_, err := client.launchArgs()
		if err == nil {
			t.Fatal("launchArgs() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "could not find codex on PATH") {
			t.Fatalf("launchArgs() error = %v, want missing codex message", err)
		}
	})
}

func TestNotifyNilParamsEncodesEmptyObject(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{})
	writer := &recordingWriteCloser{}
	client.stdin = writer

	if err := client.Notify("initialized", nil); err != nil {
		t.Fatalf("Notify() error = %v", err)
	}

	if len(writer.writes) != 1 {
		t.Fatalf("len(writer.writes) = %d, want 1", len(writer.writes))
	}

	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(writer.writes[0]), &payload); err != nil {
		t.Fatalf("json.Unmarshal(write) error = %v", err)
	}
	if payload["method"] != "initialized" {
		t.Fatalf("Notify() method = %#v, want %q", payload["method"], "initialized")
	}

	params, ok := payload["params"].(map[string]any)
	if !ok {
		t.Fatalf("Notify() params type = %T, want map[string]any", payload["params"])
	}
	if len(params) != 0 {
		t.Fatalf("Notify() params = %#v, want empty object", params)
	}
}

func TestRequestDecodeError(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{})
	writer := &recordingWriteCloser{
		writeFn: func(p []byte) error {
			var request rpcRequest
			if err := json.Unmarshal(bytes.TrimSpace(p), &request); err != nil {
				return err
			}
			client.resolvePending(request.ID, rpcResult{
				result: json.RawMessage(`{"broken"`),
			})
			return nil
		},
	}
	client.stdin = writer

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var out protocol.InitializeResponse
	err := client.Request(ctx, "test/broken", nil, &out)
	if err == nil {
		t.Fatal("Request() error = nil, want DecodeError")
	}

	decodeErr, ok := errors.AsType[*DecodeError](err)
	if !ok {
		t.Fatalf("Request() error = %T, want *DecodeError", err)
	}
	if decodeErr.Method != "test/broken" {
		t.Fatalf("DecodeError.Method = %q, want %q", decodeErr.Method, "test/broken")
	}
	if string(decodeErr.Payload) != `{"broken"` {
		t.Fatalf("DecodeError.Payload = %q, want invalid JSON payload", string(decodeErr.Payload))
	}
}

func TestHandleServerRequestErrorResponses(t *testing.T) {
	t.Parallel()

	params := json.RawMessage(`{
		"itemId":"item-1",
		"threadId":"thread-1",
		"turnId":"turn-1",
		"command":"echo hi"
	}`)

	t.Run("generic error becomes json-rpc internal error", func(t *testing.T) {
		t.Parallel()

		client := NewClient(Config{
			RequestHandler: func(_ context.Context, request ServerRequest) (any, error) {
				if _, ok := request.Payload.(*protocol.CommandExecutionRequestApprovalParams); !ok {
					t.Fatalf("request.Payload type = %T, want *protocol.CommandExecutionRequestApprovalParams", request.Payload)
				}
				return nil, errors.New("boom")
			},
		})
		writer := &recordingWriteCloser{}
		client.stdin = writer

		client.handleServerRequest(
			1,
			"item/commandExecution/requestApproval",
			params,
		)

		response := decodeResponseLine(t, writer.writes)
		if response.ID != 1 {
			t.Fatalf("response.ID = %d, want %d", response.ID, 1)
		}
		if response.Error == nil {
			t.Fatal("response.Error = nil, want JSON-RPC error")
		}
		if response.Error.Code != -32000 || response.Error.Message != "boom" {
			t.Fatalf("response.Error = %#v, want code -32000 and message boom", response.Error)
		}
	})

	t.Run("json-rpc error is preserved", func(t *testing.T) {
		t.Parallel()

		client := NewClient(Config{
			RequestHandler: func(_ context.Context, request ServerRequest) (any, error) {
				if _, ok := request.Payload.(*protocol.CommandExecutionRequestApprovalParams); !ok {
					t.Fatalf("request.Payload type = %T, want *protocol.CommandExecutionRequestApprovalParams", request.Payload)
				}
				return nil, &JSONRPCError{
					Code:    -32077,
					Message: "denied",
				}
			},
		})
		writer := &recordingWriteCloser{}
		client.stdin = writer

		client.handleServerRequest(
			2,
			"item/commandExecution/requestApproval",
			params,
		)

		response := decodeResponseLine(t, writer.writes)
		if response.ID != 2 {
			t.Fatalf("response.ID = %d, want %d", response.ID, 2)
		}
		if response.Error == nil {
			t.Fatal("response.Error = nil, want JSON-RPC error")
		}
		if response.Error.Code != -32077 || response.Error.Message != "denied" {
			t.Fatalf("response.Error = %#v, want preserved JSON-RPC error", response.Error)
		}
	})
}

func TestDecodeKnownPayloadFallback(t *testing.T) {
	t.Parallel()

	payload := decodeKnownPayload(
		"item/commandExecution/requestApproval",
		json.RawMessage(`{"itemId":1}`),
		protocol.ServerRequestFactories,
	)

	unknown, ok := payload.(UnknownPayload)
	if !ok {
		t.Fatalf("decodeKnownPayload() type = %T, want UnknownPayload", payload)
	}
	if got := unknown.Params["itemId"]; got != float64(1) {
		t.Fatalf("UnknownPayload itemId = %#v, want 1", got)
	}
}

func TestDefaultServerRequestHandler(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	methods := []string{
		"item/commandExecution/requestApproval",
		"item/fileChange/requestApproval",
	}
	for i := range methods {
		method := methods[i]
		result, err := defaultServerRequestHandler(ctx, ServerRequest{Method: method})
		if err != nil {
			t.Fatalf("defaultServerRequestHandler(%q) error = %v", method, err)
		}
		decision, ok := result.(map[string]any)["decision"]
		if !ok || decision != "accept" {
			t.Fatalf("defaultServerRequestHandler(%q) = %#v, want accept decision", method, result)
		}
	}

	result, err := defaultServerRequestHandler(ctx, ServerRequest{Method: "thread/read"})
	if err != nil {
		t.Fatalf("defaultServerRequestHandler(non-approval) error = %v", err)
	}
	if resultMap, ok := result.(map[string]any); !ok || len(resultMap) != 0 {
		t.Fatalf("defaultServerRequestHandler(non-approval) = %#v, want empty map", result)
	}
}

func TestNextNotificationClosedTransport(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{})
	client.appendStderr("stderr line")
	close(client.notifications)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.NextNotification(ctx)
	if err == nil {
		t.Fatal("NextNotification() error = nil, want TransportClosedError")
	}

	transportErr, ok := errors.AsType[*TransportClosedError](err)
	if !ok {
		t.Fatalf("NextNotification() error = %T, want *TransportClosedError", err)
	}
	if transportErr.StderrTail != "stderr line" {
		t.Fatalf("TransportClosedError.StderrTail = %q, want %q", transportErr.StderrTail, "stderr line")
	}
}

func TestInitializeRepeatedCallReturnsServerError(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "initialize.jsonl")
	client := NewClient(Config{
		LaunchArgsOverride: helperArgs(),
		Env: helperEnv(map[string]string{
			"CODEX_GO_TEST_SCENARIO": "initialize-duplicate",
			"CODEX_GO_TEST_LOG":      logPath,
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

	_, err := client.Initialize(ctx)
	if err == nil {
		t.Fatal("Initialize() second call error = nil, want JSONRPCError")
	}

	rpcErr, ok := errors.AsType[*JSONRPCError](err)
	if !ok {
		t.Fatalf("Initialize() second call error = %T, want *JSONRPCError", err)
	}
	if rpcErr.Code != -32600 {
		t.Fatalf("Initialize() second call code = %d, want %d", rpcErr.Code, -32600)
	}
	if rpcErr.Message != "Already initialized" {
		t.Fatalf("Initialize() second call message = %q, want %q", rpcErr.Message, "Already initialized")
	}

	methods := logMethods(t, readLogLines(t, logPath))
	initializeCount := 0
	initializedCount := 0
	for i := range methods {
		method := methods[i]
		switch method {
		case "initialize":
			initializeCount++
		case "initialized":
			initializedCount++
		}
	}
	if initializeCount != 2 || initializedCount != 1 {
		t.Fatalf("initialize counts = (%d, %d), want (2, 1)", initializeCount, initializedCount)
	}
}

func TestOpenRepeatedCallLeavesTransportOpen(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{
		LaunchArgsOverride: helperArgs(),
		Env: helperEnv(map[string]string{
			"CODEX_GO_TEST_SCENARIO": "initialize-duplicate",
		}),
	})
	defer func() {
		_ = client.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.Open(ctx); err != nil {
		t.Fatalf("Open() first call error = %v", err)
	}

	_, err := client.Open(ctx)
	if err == nil {
		t.Fatal("Open() second call error = nil, want JSONRPCError")
	}

	rpcErr, ok := errors.AsType[*JSONRPCError](err)
	if !ok {
		t.Fatalf("Open() second call error = %T, want *JSONRPCError", err)
	}
	if rpcErr.Message != "Already initialized" {
		t.Fatalf("Open() second call message = %q, want %q", rpcErr.Message, "Already initialized")
	}

	if _, err := client.ThreadStart(ctx, nil); err != nil {
		t.Fatalf("ThreadStart() after repeated Open() error = %v, want live transport", err)
	}
}

func TestStreamUntilMethodsStopsAtTarget(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{
		LaunchArgsOverride: helperArgs(),
		Env: helperEnv(map[string]string{
			"CODEX_GO_TEST_SCENARIO": "core",
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

	threadStarted, err := client.ThreadStart(ctx, nil)
	if err != nil {
		t.Fatalf("ThreadStart() error = %v", err)
	}

	if _, err := client.TurnStartText(ctx, threadStarted.Thread.ID, "Say hello.", nil); err != nil {
		t.Fatalf("TurnStartText() error = %v", err)
	}

	notifications, err := client.StreamUntilMethods(ctx, "turn/completed")
	if err != nil {
		t.Fatalf("StreamUntilMethods() error = %v", err)
	}
	if len(notifications) != 3 {
		t.Fatalf("len(notifications) = %d, want 3", len(notifications))
	}
	if notifications[0].Method != "item/agentMessage/delta" {
		t.Fatalf("notifications[0].Method = %q, want item/agentMessage/delta", notifications[0].Method)
	}
	if notifications[1].Method != "thread/tokenUsage/updated" {
		t.Fatalf("notifications[1].Method = %q, want thread/tokenUsage/updated", notifications[1].Method)
	}
	if notifications[2].Method != "turn/completed" {
		t.Fatalf("notifications[2].Method = %q, want turn/completed", notifications[2].Method)
	}
	if _, ok := notifications[2].Payload.(*protocol.TurnCompletedNotification); !ok {
		t.Fatalf("notifications[2].Payload type = %T, want *protocol.TurnCompletedNotification", notifications[2].Payload)
	}
}

func TestRetryOnOverloadEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("defaults to three attempts", func(t *testing.T) {
		t.Parallel()

		attempts := 0
		value, err := RetryOnOverload(context.Background(), 0, 0, 0, func(context.Context) (string, error) {
			attempts++
			if attempts < 3 {
				return "", &JSONRPCError{
					Code:    -32001,
					Message: "overloaded",
				}
			}
			return "ok", nil
		})
		if err != nil {
			t.Fatalf("RetryOnOverload() error = %v", err)
		}
		if attempts != 3 {
			t.Fatalf("RetryOnOverload() attempts = %d, want 3", attempts)
		}
		if value != "ok" {
			t.Fatalf("RetryOnOverload() value = %q, want ok", value)
		}
	})

	t.Run("does not retry non-overload errors", func(t *testing.T) {
		t.Parallel()

		wantErr := io.EOF
		attempts := 0
		_, err := RetryOnOverload(context.Background(), 4, time.Millisecond, time.Millisecond, func(context.Context) (string, error) {
			attempts++
			return "", wantErr
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("RetryOnOverload() error = %v, want %v", err, wantErr)
		}
		if attempts != 1 {
			t.Fatalf("RetryOnOverload() attempts = %d, want 1", attempts)
		}
	})

	t.Run("returns context error while waiting to retry", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		attempts := 0
		done := make(chan struct{})
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
			close(done)
		}()

		_, err := RetryOnOverload(ctx, 4, 50*time.Millisecond, 50*time.Millisecond, func(context.Context) (string, error) {
			attempts++
			return "", &JSONRPCError{
				Code:    -32001,
				Message: "overloaded",
			}
		})
		<-done

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RetryOnOverload() error = %v, want context canceled", err)
		}
		if attempts != 1 {
			t.Fatalf("RetryOnOverload() attempts = %d, want 1", attempts)
		}
	})
}

func TestErrorHelpers(t *testing.T) {
	t.Parallel()

	var nilRPC *JSONRPCError
	if got := nilRPC.Error(); got != "json-rpc error" {
		t.Fatalf("(*JSONRPCError)(nil).Error() = %q, want %q", got, "json-rpc error")
	}

	rpcErr := &JSONRPCError{Code: -32001, Message: "busy"}
	if got := rpcErr.Error(); got != "json-rpc error -32001: busy" {
		t.Fatalf("JSONRPCError.Error() = %q, want formatted error", got)
	}

	var nilTransport *TransportClosedError
	if got := nilTransport.Error(); got != "app-server transport closed" {
		t.Fatalf("(*TransportClosedError)(nil).Error() = %q, want default message", got)
	}

	transportErr := &TransportClosedError{Cause: io.EOF, StderrTail: "tail"}
	if got := transportErr.Error(); got != "app-server transport closed: EOF (stderr tail: tail)" {
		t.Fatalf("TransportClosedError.Error() = %q, want formatted message", got)
	}
	if !errors.Is(transportErr, io.EOF) {
		t.Fatalf("errors.Is(transportErr, io.EOF) = false, want true")
	}

	var nilDecode *DecodeError
	if got := nilDecode.Error(); got != "protocol decode error" {
		t.Fatalf("(*DecodeError)(nil).Error() = %q, want default message", got)
	}

	decodeErr := &DecodeError{
		Method: "turn/start",
		Cause:  io.ErrUnexpectedEOF,
	}
	if got := decodeErr.Error(); got != `protocol decode error for "turn/start": unexpected EOF` {
		t.Fatalf("DecodeError.Error() = %q, want formatted message", got)
	}
	if !errors.Is(decodeErr, io.ErrUnexpectedEOF) {
		t.Fatalf("errors.Is(decodeErr, io.ErrUnexpectedEOF) = false, want true")
	}

	wrapped := &DecodeError{Cause: rpcErr}
	target, ok := errors.AsType[*JSONRPCError](wrapped)
	if !ok {
		t.Fatal("errors.AsType[*JSONRPCError]() = false, want true")
	}
	if target != rpcErr {
		t.Fatalf("errors.AsType[*JSONRPCError]() target = %#v, want %#v", target, rpcErr)
	}
}

func decodeResponseLine(t *testing.T, writes [][]byte) rpcResponse {
	t.Helper()

	if len(writes) != 1 {
		t.Fatalf("len(writes) = %d, want 1", len(writes))
	}

	var response rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(writes[0]), &response); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	return response
}
