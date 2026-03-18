package codexappserver

import (
	"encoding/json"
	"errors"
	"fmt"
)

// JSONRPCError is a JSON-RPC error returned by codex app-server.
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *JSONRPCError) Error() string {
	if e == nil {
		return "json-rpc error"
	}
	return fmt.Sprintf("json-rpc error %d: %s", e.Code, e.Message)
}

// IsOverload reports whether err is the app-server retryable overload error.
func IsOverload(err error) bool {
	rpcErr, ok := errors.AsType[*JSONRPCError](err)
	return ok && rpcErr.Code == -32001
}

// TransportClosedError reports that the app-server transport stopped before the
// SDK finished the current operation.
type TransportClosedError struct {
	Cause      error
	StderrTail string
}

func (e *TransportClosedError) Error() string {
	if e == nil {
		return "app-server transport closed"
	}
	if e.StderrTail == "" {
		if e.Cause != nil {
			return fmt.Sprintf("app-server transport closed: %v", e.Cause)
		}
		return "app-server transport closed"
	}
	if e.Cause != nil {
		return fmt.Sprintf("app-server transport closed: %v (stderr tail: %s)", e.Cause, e.StderrTail)
	}
	return fmt.Sprintf("app-server transport closed (stderr tail: %s)", e.StderrTail)
}

func (e *TransportClosedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// DecodeError reports that a payload could not be decoded into the expected
// Go type.
type DecodeError struct {
	Method  string
	Payload json.RawMessage
	Cause   error
}

func (e *DecodeError) Error() string {
	if e == nil {
		return "protocol decode error"
	}
	if e.Method == "" {
		return fmt.Sprintf("protocol decode error: %v", e.Cause)
	}
	return fmt.Sprintf("protocol decode error for %q: %v", e.Method, e.Cause)
}

func (e *DecodeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
