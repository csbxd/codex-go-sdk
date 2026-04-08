package codex

import (
	"context"
	"encoding/json"

	"github.com/csbxd/codex-go-sdk/codex/protocol"
)

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
	for i := range methods {
		method := methods[i]
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
