package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/csbxd/codex-go-sdk/codex/protocol"
)

const (
	defaultClientName         = "codex_go_sdk"
	defaultClientTitle        = "Codex Go SDK"
	defaultClientVersion      = "0.1.0"
	defaultNotificationBuffer = 64
	defaultStderrTailLines    = 400
)

// Config controls how the SDK launches and communicates with codex app-server.
type Config struct {
	CodexBin           string
	LaunchArgsOverride []string
	ConfigOverrides    []string
	CWD                string
	Env                map[string]string
	ClientName         string
	ClientTitle        string
	ClientVersion      string
	ExperimentalAPI    bool
	NotificationBuffer int
	StderrTailLines    int
	RequestHandler     ServerRequestHandler
}

// Notification is a decoded server notification.
type Notification struct {
	Method  string
	Payload any
}

// UnknownPayload preserves notifications or server requests that the SDK does
// not have a generated type for, or that failed to decode into the expected
// generated type.
type UnknownPayload struct {
	Params map[string]any
	Raw    json.RawMessage
}

// ServerRequest is a decoded server-initiated JSON-RPC request.
type ServerRequest struct {
	ID      int64
	Method  string
	Payload any
}

// ServerRequestHandler decides how the client responds to approval and other
// server-initiated requests.
type ServerRequestHandler func(context.Context, ServerRequest) (any, error)

// Client is a synchronous typed JSON-RPC client for codex app-server over
// stdio.
type Client struct {
	config Config

	clientCtx    context.Context
	clientCancel context.CancelFunc

	writeMu       sync.Mutex
	stateMu       sync.RWMutex
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	processDone   chan struct{}
	stderrDone    chan struct{}
	transportDone chan struct{}
	processErr    error

	pendingMu sync.Mutex
	pending   map[int64]chan rpcResult

	notifications chan Notification

	stderrMu    sync.Mutex
	stderrLines []string

	transportErrMu sync.RWMutex
	transportErr   error

	nextID atomic.Int64

	transportOnce sync.Once
	closeOnce     sync.Once
}

type rpcEnvelope struct {
	ID     *int64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *JSONRPCError   `json:"error,omitempty"`
}

type rpcRequest struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type rpcNotification struct {
	Method string `json:"method"`
	Params any    `json:"params"`
}

type rpcResponse struct {
	ID     int64         `json:"id"`
	Result any           `json:"result,omitempty"`
	Error  *JSONRPCError `json:"error,omitempty"`
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

// NewClient constructs a client with the provided config.
func NewClient(config Config) *Client {
	if config.ClientName == "" {
		config.ClientName = defaultClientName
	}
	if config.ClientTitle == "" {
		config.ClientTitle = defaultClientTitle
	}
	if config.ClientVersion == "" {
		config.ClientVersion = defaultClientVersion
	}
	if config.NotificationBuffer <= 0 {
		config.NotificationBuffer = defaultNotificationBuffer
	}
	if config.StderrTailLines <= 0 {
		config.StderrTailLines = defaultStderrTailLines
	}
	if config.RequestHandler == nil {
		config.RequestHandler = defaultServerRequestHandler
	}

	clientCtx, cancel := context.WithCancel(context.Background())
	return &Client{
		config:        config,
		clientCtx:     clientCtx,
		clientCancel:  cancel,
		pending:       make(map[int64]chan rpcResult),
		notifications: make(chan Notification, config.NotificationBuffer),
	}
}

// MarshalUserInput converts an input item into the generated protocol type.
func MarshalUserInput(value any) (protocol.UserInput, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return protocol.UserInput{}, err
	}

	var input protocol.UserInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return protocol.UserInput{}, err
	}
	if !input.Type.IsValid() {
		if input.Type == "" {
			return protocol.UserInput{}, errors.New("user input type is empty")
		}
		return protocol.UserInput{}, fmt.Errorf("unknown user input type %q", input.Type)
	}
	return input, nil
}

// MustUserInput converts an input item into the generated protocol type and
// panics if it cannot be marshaled.
func MustUserInput(value any) protocol.UserInput {
	input, err := MarshalUserInput(value)
	if err != nil {
		panic(err)
	}
	return input
}

// TextInput constructs the common text-only turn input payload.
func TextInput(text string) []protocol.UserInput {
	return []protocol.UserInput{
		MustUserInput(map[string]any{
			"type": "text",
			"text": text,
		}),
	}
}

// Start launches the configured codex app-server process.
func (c *Client) Start(_ context.Context) error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	if c.cmd != nil {
		return nil
	}

	args, err := c.launchArgs()
	if err != nil {
		return err
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = c.processEnv()
	if c.config.CWD != "" {
		cmd.Dir = c.config.CWD
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	c.cmd = cmd
	c.stdin = stdin
	c.processDone = make(chan struct{})
	c.stderrDone = make(chan struct{})
	c.transportDone = make(chan struct{})
	c.processErr = nil
	c.transportErr = nil

	go c.runTransport(cmd, stdout, stderr)

	return nil
}

// Open starts the process if needed, performs initialize, and emits the
// required initialized notification.
func (c *Client) Open(ctx context.Context) (protocol.InitializeResponse, error) {
	if err := c.Start(ctx); err != nil {
		return protocol.InitializeResponse{}, err
	}

	resp, err := c.Initialize(ctx)
	if err != nil {
		rpcErr, ok := errors.AsType[*JSONRPCError](err)
		if !ok || rpcErr.Message != "Already initialized" {
			_ = c.Close()
		}
		return protocol.InitializeResponse{}, err
	}
	return resp, nil
}

// Close stops the child process and releases resources.
func (c *Client) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		c.clientCancel()
		c.closeStdin()

		c.stateMu.RLock()
		cmd := c.cmd
		transportDone := c.transportDone
		c.stateMu.RUnlock()

		if cmd == nil || cmd.Process == nil {
			return
		}

		_ = cmd.Process.Signal(os.Interrupt)

		if transportDone != nil {
			select {
			case <-transportDone:
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				<-transportDone
			}
		}

		closeErr = c.processExitErr()
	})

	if closeErr == nil || strings.Contains(closeErr.Error(), "signal: interrupt") {
		return nil
	}
	return closeErr
}

// Initialize performs the required initialize request followed by the
// initialized notification.
func (c *Client) Initialize(ctx context.Context) (protocol.InitializeResponse, error) {
	params := protocol.InitializeParams{
		ClientInfo: protocol.ClientInfo{
			Name:    c.config.ClientName,
			Title:   new(c.config.ClientTitle),
			Version: c.config.ClientVersion,
		},
		Capabilities: &protocol.InitializeCapabilities{
			ExperimentalApi: new(c.config.ExperimentalAPI),
		},
	}

	var resp protocol.InitializeResponse
	if err := c.Request(ctx, "initialize", params, &resp); err != nil {
		return protocol.InitializeResponse{}, err
	}
	if err := c.Notify("initialized", map[string]any{}); err != nil {
		return protocol.InitializeResponse{}, err
	}

	return resp, nil
}

// Notify writes a JSON-RPC notification to codex app-server.
func (c *Client) Notify(method string, params any) error {
	if params == nil {
		params = map[string]any{}
	}
	return c.writeJSON(rpcNotification{
		Method: method,
		Params: params,
	})
}

// Request sends a JSON-RPC request and decodes the result into out.
func (c *Client) Request(ctx context.Context, method string, params any, out any) error {
	if params == nil {
		params = map[string]any{}
	}

	requestID := c.nextID.Add(1)
	resultCh := make(chan rpcResult, 1)
	c.registerPending(requestID, resultCh)
	defer c.unregisterPending(requestID)

	if err := c.writeJSON(rpcRequest{
		ID:     requestID,
		Method: method,
		Params: params,
	}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			return result.err
		}
		if out == nil || len(result.result) == 0 {
			return nil
		}
		if err := json.Unmarshal(result.result, out); err != nil {
			return &DecodeError{
				Method:  method,
				Payload: append(json.RawMessage(nil), result.result...),
				Cause:   err,
			}
		}
		return nil
	}
}

// Call decodes a JSON-RPC result into T.
func Call[T any](ctx context.Context, client *Client, method string, params any) (T, error) {
	var out T
	err := client.Request(ctx, method, params, &out)
	return out, err
}

// NextNotification waits for the next server notification.
func (c *Client) NextNotification(ctx context.Context) (Notification, error) {
	select {
	case <-ctx.Done():
		return Notification{}, ctx.Err()
	case notification, ok := <-c.notifications:
		if !ok {
			return Notification{}, c.transportClosed()
		}
		return notification, nil
	}
}

// WaitForTurnCompleted reads notifications until the specified turn completes.
func (c *Client) WaitForTurnCompleted(
	ctx context.Context,
	turnID string,
) (*protocol.TurnCompletedNotification, error) {
	for {
		notification, err := c.NextNotification(ctx)
		if err != nil {
			return nil, err
		}

		payload, ok := notification.Payload.(*protocol.TurnCompletedNotification)
		if ok && payload.Turn.ID == turnID {
			return payload, nil
		}
	}
}

// StreamUntilMethods collects notifications until one of the target methods
// arrives.
func (c *Client) StreamUntilMethods(
	ctx context.Context,
	methods ...string,
) ([]Notification, error) {
	targets := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		targets[method] = struct{}{}
	}

	var out []Notification
	for {
		notification, err := c.NextNotification(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, notification)
		if _, ok := targets[notification.Method]; ok {
			return out, nil
		}
	}
}

// ThreadStart invokes thread/start.
func (c *Client) ThreadStart(
	ctx context.Context,
	params *protocol.ThreadStartParams,
) (protocol.ThreadStartResponse, error) {
	return Call[protocol.ThreadStartResponse](ctx, c, "thread/start", valueOrEmpty(params))
}

// ThreadResume invokes thread/resume.
func (c *Client) ThreadResume(
	ctx context.Context,
	params *protocol.ThreadResumeParams,
) (protocol.ThreadResumeResponse, error) {
	return Call[protocol.ThreadResumeResponse](ctx, c, "thread/resume", mustValue(params))
}

// ThreadList invokes thread/list.
func (c *Client) ThreadList(
	ctx context.Context,
	params *protocol.ThreadListParams,
) (protocol.ThreadListResponse, error) {
	return Call[protocol.ThreadListResponse](ctx, c, "thread/list", valueOrEmpty(params))
}

// ThreadRead invokes thread/read.
func (c *Client) ThreadRead(
	ctx context.Context,
	params *protocol.ThreadReadParams,
) (protocol.ThreadReadResponse, error) {
	return Call[protocol.ThreadReadResponse](ctx, c, "thread/read", mustValue(params))
}

// ThreadFork invokes thread/fork.
func (c *Client) ThreadFork(
	ctx context.Context,
	params *protocol.ThreadForkParams,
) (protocol.ThreadForkResponse, error) {
	return Call[protocol.ThreadForkResponse](ctx, c, "thread/fork", mustValue(params))
}

// ThreadArchive invokes thread/archive.
func (c *Client) ThreadArchive(
	ctx context.Context,
	params *protocol.ThreadArchiveParams,
) (protocol.ThreadArchiveResponse, error) {
	return Call[protocol.ThreadArchiveResponse](ctx, c, "thread/archive", mustValue(params))
}

// ThreadUnarchive invokes thread/unarchive.
func (c *Client) ThreadUnarchive(
	ctx context.Context,
	params *protocol.ThreadUnarchiveParams,
) (protocol.ThreadUnarchiveResponse, error) {
	return Call[protocol.ThreadUnarchiveResponse](ctx, c, "thread/unarchive", mustValue(params))
}

// ThreadSetName invokes thread/name/set.
func (c *Client) ThreadSetName(
	ctx context.Context,
	params *protocol.ThreadSetNameParams,
) (protocol.ThreadSetNameResponse, error) {
	return Call[protocol.ThreadSetNameResponse](ctx, c, "thread/name/set", mustValue(params))
}

// ThreadCompact invokes thread/compact/start.
func (c *Client) ThreadCompact(
	ctx context.Context,
	params *protocol.ThreadCompactStartParams,
) (protocol.ThreadCompactStartResponse, error) {
	return Call[protocol.ThreadCompactStartResponse](ctx, c, "thread/compact/start", mustValue(params))
}

// TurnStart invokes turn/start.
func (c *Client) TurnStart(
	ctx context.Context,
	params *protocol.TurnStartParams,
) (protocol.TurnStartResponse, error) {
	return Call[protocol.TurnStartResponse](ctx, c, "turn/start", mustValue(params))
}

// TurnStartText invokes turn/start with a single text input item.
func (c *Client) TurnStartText(
	ctx context.Context,
	threadID string,
	text string,
	params *protocol.TurnStartParams,
) (protocol.TurnStartResponse, error) {
	cloned := protocol.TurnStartParams{}
	if params != nil {
		cloned = *params
	}
	cloned.ThreadId = threadID
	cloned.Input = TextInput(text)
	return c.TurnStart(ctx, &cloned)
}

// TurnInterrupt invokes turn/interrupt.
func (c *Client) TurnInterrupt(
	ctx context.Context,
	params *protocol.TurnInterruptParams,
) (protocol.TurnInterruptResponse, error) {
	return Call[protocol.TurnInterruptResponse](ctx, c, "turn/interrupt", mustValue(params))
}

// TurnSteer invokes turn/steer.
func (c *Client) TurnSteer(
	ctx context.Context,
	params *protocol.TurnSteerParams,
) (protocol.TurnSteerResponse, error) {
	return Call[protocol.TurnSteerResponse](ctx, c, "turn/steer", mustValue(params))
}

// TurnSteerText invokes turn/steer with a single text input item.
func (c *Client) TurnSteerText(
	ctx context.Context,
	threadID string,
	expectedTurnID string,
	text string,
) (protocol.TurnSteerResponse, error) {
	return c.TurnSteer(ctx, &protocol.TurnSteerParams{
		ThreadId:       threadID,
		ExpectedTurnId: expectedTurnID,
		Input:          TextInput(text),
	})
}

// ModelList invokes model/list.
func (c *Client) ModelList(
	ctx context.Context,
	params *protocol.ModelListParams,
) (protocol.ModelListResponse, error) {
	return Call[protocol.ModelListResponse](ctx, c, "model/list", valueOrEmpty(params))
}

func (c *Client) launchArgs() ([]string, error) {
	if len(c.config.LaunchArgsOverride) > 0 {
		return append([]string(nil), c.config.LaunchArgsOverride...), nil
	}

	bin := c.config.CodexBin
	if bin == "" {
		resolved, err := exec.LookPath("codex")
		if err != nil {
			return nil, fmt.Errorf("could not find codex on PATH: %w", err)
		}
		bin = resolved
	}

	args := []string{bin}
	for _, override := range c.config.ConfigOverrides {
		args = append(args, "--config", override)
	}
	args = append(args, "app-server", "--listen", "stdio://")
	return args, nil
}

func (c *Client) processEnv() []string {
	envMap := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			envMap[key] = value
		}
	}
	maps.Copy(envMap, c.config.Env)

	env := make([]string, 0, len(envMap))
	for key, value := range envMap {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	return env
}

// runTransport serializes shutdown so stderr is fully drained before Wait
// closes the child pipes.
func (c *Client) runTransport(cmd *exec.Cmd, stdout io.Reader, stderr io.Reader) {
	stdoutErrCh := make(chan error, 1)

	go func() {
		stdoutErrCh <- c.readLoop(stdout)
	}()
	go c.drainStderr(stderr)

	stdoutErr := <-stdoutErrCh
	if stdoutErr != nil && !errors.Is(stdoutErr, io.EOF) && !errors.Is(stdoutErr, os.ErrClosed) {
		c.requestTransportStop()
	}

	c.waitForStderrDrain()

	waitErr := cmd.Wait()
	c.setProcessErr(waitErr)

	c.finishTransport(&TransportClosedError{
		Cause:      transportCause(stdoutErr, waitErr),
		StderrTail: c.stderrTail(),
	})
}

func (c *Client) readLoop(stdout io.Reader) error {
	reader := bufio.NewReader(stdout)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if len(bytes.TrimSpace(line)) > 0 {
				if dispatchErr := c.handleRawLine(bytes.TrimSpace(line)); dispatchErr != nil {
					err = dispatchErr
				}
			}
			return err
		}

		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if dispatchErr := c.handleRawLine(line); dispatchErr != nil {
			return dispatchErr
		}
	}
}

func (c *Client) handleRawLine(line []byte) error {
	var message rpcEnvelope
	if err := json.Unmarshal(line, &message); err != nil {
		return &DecodeError{
			Payload: append(json.RawMessage(nil), line...),
			Cause:   err,
		}
	}

	if message.Method != "" && message.ID != nil {
		go c.handleServerRequest(*message.ID, message.Method, message.Params)
		return nil
	}
	if message.Method != "" {
		c.notifications <- Notification{
			Method:  message.Method,
			Payload: decodeKnownPayload(message.Method, message.Params, protocol.NotificationFactories),
		}
		return nil
	}
	if message.ID != nil {
		requestID := *message.ID
		var err error
		if message.Error != nil {
			err = message.Error
		}
		c.resolvePending(requestID, rpcResult{
			result: append(json.RawMessage(nil), message.Result...),
			err:    err,
		})
	}
	return nil
}

func (c *Client) handleServerRequest(id int64, method string, params json.RawMessage) {
	request := ServerRequest{
		ID:      id,
		Method:  method,
		Payload: decodeKnownPayload(method, params, protocol.ServerRequestFactories),
	}

	result, err := c.config.RequestHandler(c.clientCtx, request)
	if err != nil {
		rpcErr, ok := errors.AsType[*JSONRPCError](err)
		if !ok {
			rpcErr = &JSONRPCError{
				Code:    -32000,
				Message: err.Error(),
			}
		}
		_ = c.writeJSON(rpcResponse{
			ID:    id,
			Error: rpcErr,
		})
		return
	}
	if result == nil {
		result = map[string]any{}
	}
	_ = c.writeJSON(rpcResponse{
		ID:     id,
		Result: result,
	})
}

func (c *Client) writeJSON(payload any) error {
	c.stateMu.RLock()
	stdin := c.stdin
	c.stateMu.RUnlock()

	if stdin == nil {
		return c.transportClosed()
	}

	bytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if _, err := stdin.Write(append(bytes, '\n')); err != nil {
		return c.transportClosedFrom(err)
	}
	return nil
}

func (c *Client) drainStderr(stderr io.Reader) {
	defer c.markStderrDone()

	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		c.appendStderr(scanner.Text())
	}
}

func (c *Client) appendStderr(line string) {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()

	c.stderrLines = append(c.stderrLines, line)
	if len(c.stderrLines) > c.config.StderrTailLines {
		c.stderrLines = c.stderrLines[len(c.stderrLines)-c.config.StderrTailLines:]
	}
}

func (c *Client) stderrTail() string {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	return strings.Join(c.stderrLines, "\n")
}

func (c *Client) registerPending(requestID int64, resultCh chan rpcResult) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pending[requestID] = resultCh
}

func (c *Client) unregisterPending(requestID int64) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pending, requestID)
}

func (c *Client) resolvePending(requestID int64, result rpcResult) {
	c.pendingMu.Lock()
	resultCh, ok := c.pending[requestID]
	if ok {
		delete(c.pending, requestID)
	}
	c.pendingMu.Unlock()

	if ok {
		resultCh <- result
	}
}

func (c *Client) failPending(err error) {
	c.pendingMu.Lock()
	pending := c.pending
	c.pending = make(map[int64]chan rpcResult)
	c.pendingMu.Unlock()

	for _, resultCh := range pending {
		resultCh <- rpcResult{err: err}
	}
}

func (c *Client) finishTransport(finalErr error) {
	c.transportOnce.Do(func() {
		c.clientCancel()
		c.closeStdin()
		c.setTransportErr(finalErr)
		c.failPending(finalErr)
		close(c.notifications)

		c.stateMu.RLock()
		transportDone := c.transportDone
		c.stateMu.RUnlock()
		if transportDone != nil {
			close(transportDone)
		}
	})
}

func (c *Client) transportClosed() error {
	if err := c.getTransportErr(); err != nil {
		return err
	}

	c.stateMu.RLock()
	transportDone := c.transportDone
	c.stateMu.RUnlock()
	if transportDone != nil {
		<-transportDone
	}
	if err := c.getTransportErr(); err != nil {
		return err
	}

	return &TransportClosedError{StderrTail: c.stderrTail()}
}

func (c *Client) transportClosedFrom(cause error) error {
	if err := c.getTransportErr(); err != nil {
		return err
	}

	c.waitForTransportDone()
	if err := c.getTransportErr(); err != nil {
		return err
	}

	return &TransportClosedError{
		Cause:      cause,
		StderrTail: c.stderrTail(),
	}
}

func (c *Client) closeStdin() {
	c.stateMu.Lock()
	stdin := c.stdin
	c.stdin = nil
	c.stateMu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
}

func (c *Client) requestTransportStop() {
	c.closeStdin()

	c.stateMu.RLock()
	cmd := c.cmd
	c.stateMu.RUnlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
	}
}

func (c *Client) markStderrDone() {
	c.stateMu.RLock()
	stderrDone := c.stderrDone
	c.stateMu.RUnlock()

	if stderrDone != nil {
		close(stderrDone)
	}
}

func (c *Client) waitForStderrDrain() {
	c.stateMu.RLock()
	stderrDone := c.stderrDone
	c.stateMu.RUnlock()

	if stderrDone != nil {
		<-stderrDone
	}
}

func (c *Client) waitForTransportDone() {
	c.stateMu.RLock()
	transportDone := c.transportDone
	c.stateMu.RUnlock()

	if transportDone != nil {
		<-transportDone
	}
}

func (c *Client) processExitErr() error {
	c.stateMu.RLock()
	processDone := c.processDone
	c.stateMu.RUnlock()

	if processDone != nil {
		<-processDone
	}

	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.processErr
}

func (c *Client) setTransportErr(err error) {
	c.transportErrMu.Lock()
	defer c.transportErrMu.Unlock()
	if c.transportErr == nil {
		c.transportErr = err
	}
}

func (c *Client) getTransportErr() error {
	c.transportErrMu.RLock()
	defer c.transportErrMu.RUnlock()
	return c.transportErr
}

func (c *Client) setProcessErr(err error) {
	c.stateMu.Lock()
	c.processErr = err
	processDone := c.processDone
	c.stateMu.Unlock()

	if processDone != nil {
		close(processDone)
	}
}

func transportCause(cause error, processErr error) error {
	if cause == nil {
		return processErr
	}
	if processErr != nil && (errors.Is(cause, io.EOF) || errors.Is(cause, io.ErrClosedPipe) || errors.Is(cause, os.ErrClosed)) {
		return processErr
	}
	return cause
}

func decodeKnownPayload(
	method string,
	raw json.RawMessage,
	factories map[string]func() any,
) any {
	factory := factories[method]
	if factory == nil {
		return unknownPayload(raw)
	}

	payload := factory()
	if err := json.Unmarshal(raw, payload); err != nil {
		return unknownPayload(raw)
	}
	return payload
}

func unknownPayload(raw json.RawMessage) UnknownPayload {
	params := make(map[string]any)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &params)
	}
	return UnknownPayload{
		Params: params,
		Raw:    append(json.RawMessage(nil), raw...),
	}
}

func defaultServerRequestHandler(
	_ context.Context,
	request ServerRequest,
) (any, error) {
	switch request.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		return map[string]any{"decision": "accept"}, nil
	default:
		return map[string]any{}, nil
	}
}

func valueOrEmpty[T any](value *T) any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func mustValue[T any](value *T) any {
	if value == nil {
		return map[string]any{}
	}
	return value
}
