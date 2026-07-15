package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/irc"
)

func TestUserMetaFrontendDeliveryIsBoundedAndCoalesced(t *testing.T) {
	t.Parallel()

	emitted := make(chan []map[string]interface{}, 4)
	a := &App{
		userMetaPending: make(map[string]map[string]interface{}),
		emitFn: func(name string, data ...any) {
			if name != "usermeta-event" || len(data) != 1 {
				return
			}
			envelope, ok := data[0].(map[string]interface{})
			if !ok {
				t.Errorf("unexpected envelope type %T", data[0])
				return
			}
			updates, ok := envelope["updates"].([]map[string]interface{})
			if !ok {
				t.Errorf("unexpected updates type %T", envelope["updates"])
				return
			}
			emitted <- updates
		},
	}

	for i := 0; i < 600; i++ {
		nick := fmt.Sprintf("user-%03d", i)
		a.queueUserMetaFrontend(events.Event{
			Type: irc.EventUserMetaChanged,
			Data: map[string]interface{}{
				"networkId": int64(1),
				"nickname":  nick,
				"account":   "first",
			},
		})
	}
	// A later snapshot for the same network/nick must replace the queued one.
	a.queueUserMetaFrontend(events.Event{
		Type: irc.EventUserMetaChanged,
		Data: map[string]interface{}{
			"networkId": int64(1),
			"nickname":  "user-000",
			"account":   "latest",
		},
	})

	seen := make(map[string]map[string]interface{}, 600)
	deadline := time.After(time.Second)
	for len(seen) < 600 {
		select {
		case batch := <-emitted:
			if len(batch) > userMetaFrontendBatchSize {
				t.Fatalf("batch contained %d updates; limit is %d", len(batch), userMetaFrontendBatchSize)
			}
			for _, update := range batch {
				nick, _ := update["nickname"].(string)
				seen[nick] = update
			}
		case <-deadline:
			t.Fatalf("timed out after receiving %d of 600 coalesced updates", len(seen))
		}
	}

	if got := seen["user-000"]["account"]; got != "latest" {
		t.Fatalf("coalesced snapshot account = %v, want latest", got)
	}
}
