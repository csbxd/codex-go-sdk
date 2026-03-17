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

	client := codexappserver.NewClient(codexappserver.Config{})
	defer func() {
		_ = client.Close()
	}()

	if _, err := client.Open(ctx); err != nil {
		log.Fatal(err)
	}

	started, err := client.ThreadStart(ctx, &protocol.ThreadStartParams{
		Model: codexappserver.Ptr("gpt-5"),
	})
	if err != nil {
		log.Fatal(err)
	}

	turnStarted, err := client.TurnStartText(
		ctx,
		started.Thread.ID,
		"Say hello in one sentence.",
		&protocol.TurnStartParams{
			Effort: codexappserver.Ptr(protocol.ReasoningEffortMedium),
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	completed, err := client.WaitForTurnCompleted(ctx, turnStarted.Turn.ID)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("thread=%s turn=%s status=%s\n", started.Thread.ID, completed.Turn.ID, completed.Turn.Status)
}
