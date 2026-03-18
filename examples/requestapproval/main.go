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
	approvalPolicy := protocol.AskForApproval([]byte(`"untrusted"`))

	client := codexappserver.NewClient(codexappserver.Config{
		RequestHandler: func(_ context.Context, request codexappserver.ServerRequest) (any, error) {
			fmt.Printf("server request: %s (%T)\n", request.Method, request.Payload)
			return map[string]any{"decision": "accept"}, nil
		},
	})
	defer func() {
		_ = client.Close()
	}()

	if _, err := client.Open(ctx); err != nil {
		log.Fatal(err)
	}

	started, err := client.ThreadStart(ctx, &protocol.ThreadStartParams{
		ApprovalPolicy: &approvalPolicy,
	})
	if err != nil {
		log.Fatal(err)
	}

	_, err = client.TurnStartText(
		ctx,
		started.Thread.ID,
		"Create a file named approval_probe.txt in the current directory with the exact text hello.",
		&protocol.TurnStartParams{
			ApprovalPolicy: &approvalPolicy,
			Effort:         new(protocol.ReasoningEffortMedium),
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("waiting for approvals and turn notifications...")
	_, err = client.StreamUntilMethods(ctx, "turn/completed")
	if err != nil {
		log.Fatal(err)
	}
}
