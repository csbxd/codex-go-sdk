package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestAskForApprovalJSON(t *testing.T) {
	t.Parallel()

	boolPtr := func(v bool) *bool {
		return &v
	}

	tests := []struct {
		name string
		in   AskForApproval
		want string
	}{
		{
			name: "string variant",
			in:   AskForApproval{Kind: AskForApprovalKindUntrusted},
			want: `"untrusted"`,
		},
		{
			name: "object variant",
			in: AskForApproval{
				Granular: &AskForApprovalGranular{
					MCPElicitations:    true,
					RequestPermissions: boolPtr(false),
					Rules:              true,
					SandboxApproval:    false,
				},
			},
			want: `{"granular":{"mcp_elicitations":true,"request_permissions":false,"rules":true,"sandbox_approval":false}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(test.in)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(data) != test.want {
				t.Fatalf("json.Marshal() = %s, want %s", data, test.want)
			}

			var got AskForApproval
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.in) {
				t.Fatalf("json round-trip = %#v, want %#v", got, test.in)
			}
		})
	}
}

func TestRequestIDJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   RequestId
		want string
	}{
		{
			name: "string variant",
			in:   RequestIdFromString("req-1"),
			want: `"req-1"`,
		},
		{
			name: "integer variant",
			in:   RequestIdFromInteger(42),
			want: `42`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(test.in)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(data) != test.want {
				t.Fatalf("json.Marshal() = %s, want %s", data, test.want)
			}

			var got RequestId
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.in) {
				t.Fatalf("json round-trip = %#v, want %#v", got, test.in)
			}
		})
	}
}

func TestSessionSourceJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   SessionSource
		want string
	}{
		{
			name: "string variant",
			in:   SessionSource{Kind: SessionSourceKindAppServer},
			want: `"appServer"`,
		},
		{
			name: "nested object variant",
			in: SessionSource{
				SubAgent: &SubAgentSource{
					ThreadSpawn: &SubAgentSourceThreadSpawn{
						Depth:          2,
						ParentThreadID: "thread-123",
					},
				},
			},
			want: `{"subAgent":{"thread_spawn":{"depth":2,"parent_thread_id":"thread-123"}}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(test.in)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(data) != test.want {
				t.Fatalf("json.Marshal() = %s, want %s", data, test.want)
			}

			var got SessionSource
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.in) {
				t.Fatalf("json round-trip = %#v, want %#v", got, test.in)
			}
		})
	}
}
