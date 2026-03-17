package main

import (
	"context"
	"encoding/json"
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

	started, err := client.ThreadStart(ctx, &protocol.ThreadStartParams{})
	if err != nil {
		log.Fatal(err)
	}

	turnStarted, err := client.TurnStartText(
		ctx,
		started.Thread.ID,
		"Describe what you are doing as you work.",
		&protocol.TurnStartParams{
			Effort: codexappserver.Ptr(protocol.ReasoningEffortMedium),
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	for {
		notification, err := client.NextNotification(ctx)
		if err != nil {
			log.Fatal(err)
		}
		marshal, _ := json.Marshal(notification)

		fmt.Printf("%s\n", string(marshal))
		continue
		switch payload := notification.Payload.(type) {
		case *protocol.AgentMessageDeltaNotification:
			fmt.Printf("delta: %s\n", payload.Delta)
		case *protocol.ThreadTokenUsageUpdatedNotification:
			fmt.Printf("usage total=%d\n", payload.TokenUsage.Total.TotalTokens)
		case *protocol.TurnCompletedNotification:
			fmt.Printf("turn %s finished with status=%s\n", payload.Turn.ID, payload.Turn.Status)
			if payload.Turn.ID == turnStarted.Turn.ID {
				return
			}
		default:
			fmt.Printf("notification: %s\n", notification.Method)
		}
	}
}
