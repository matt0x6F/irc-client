package main

import (
	"testing"

	"github.com/matt0x6f/irc-client/internal/notification"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

func TestRouteNotificationResponse(t *testing.T) {
	type emitted struct {
		name string
		data []any
	}
	newDeps := func(e *[]emitted, sent *[]string, recon *[]int64) notifyResponseDeps {
		return notifyResponseDeps{
			emit:        func(name string, data ...any) { *e = append(*e, emitted{name, data}) },
			sendMessage: func(id int64, target, msg string) error { *sent = append(*sent, target+"|"+msg); return nil },
			reconnect:   func(id int64) error { *recon = append(*recon, id); return nil },
		}
	}

	t.Run("default action navigates", func(t *testing.T) {
		var e []emitted
		var sent []string
		var recon []int64
		routeNotificationResponse(notifications.NotificationResponse{
			ActionIdentifier: notifications.DefaultActionIdentifier,
			UserInfo:         map[string]interface{}{"networkId": "7", "target": "pm:alice"},
		}, newDeps(&e, &sent, &recon))
		if len(e) != 1 || e[0].name != "notification:navigate" {
			t.Fatalf("want one notification:navigate emit, got %+v", e)
		}
	})

	t.Run("mark read emits", func(t *testing.T) {
		var e []emitted
		var sent []string
		var recon []int64
		routeNotificationResponse(notifications.NotificationResponse{
			ActionIdentifier: notifyActionMarkRead,
			UserInfo:         map[string]interface{}{"networkId": "7", "target": "#go"},
		}, newDeps(&e, &sent, &recon))
		if len(e) != 1 || e[0].name != "notification:markRead" {
			t.Fatalf("want notification:markRead, got %+v", e)
		}
	})

	t.Run("reply strips pm prefix and sends to nick", func(t *testing.T) {
		var e []emitted
		var sent []string
		var recon []int64
		routeNotificationResponse(notifications.NotificationResponse{
			ActionIdentifier: notifyActionReply,
			UserText:         "hi back",
			UserInfo:         map[string]interface{}{"networkId": "7", "target": "pm:alice"},
		}, newDeps(&e, &sent, &recon))
		if len(sent) != 1 || sent[0] != "alice|hi back" {
			t.Fatalf("want reply to alice, got %+v", sent)
		}
	})

	t.Run("reply to channel mention keeps channel target", func(t *testing.T) {
		var e []emitted
		var sent []string
		var recon []int64
		routeNotificationResponse(notifications.NotificationResponse{
			ActionIdentifier: notifyActionReply,
			UserText:         "yo",
			UserInfo:         map[string]interface{}{"networkId": "7", "target": "#go"},
		}, newDeps(&e, &sent, &recon))
		if len(sent) != 1 || sent[0] != "#go|yo" {
			t.Fatalf("want reply to #go, got %+v", sent)
		}
	})

	t.Run("empty reply text is ignored", func(t *testing.T) {
		var e []emitted
		var sent []string
		var recon []int64
		routeNotificationResponse(notifications.NotificationResponse{
			ActionIdentifier: notifyActionReply,
			UserInfo:         map[string]interface{}{"networkId": "7", "target": "pm:alice"},
		}, newDeps(&e, &sent, &recon))
		if len(sent) != 0 {
			t.Fatalf("empty reply must not send, got %+v", sent)
		}
	})

	t.Run("reconnect parses network id", func(t *testing.T) {
		var e []emitted
		var sent []string
		var recon []int64
		routeNotificationResponse(notifications.NotificationResponse{
			ActionIdentifier: notifyActionReconnect,
			UserInfo:         map[string]interface{}{"networkId": "42", "target": ""},
		}, newDeps(&e, &sent, &recon))
		if len(recon) != 1 || recon[0] != 42 {
			t.Fatalf("want reconnect(42), got %+v", recon)
		}
	})
}

func TestToOptionsAndCategory(t *testing.T) {
	d := &wailsDelivery{}
	opts := d.toOptions(notificationFixture())
	if opts.ID != "id1" || opts.CategoryID != "message" || opts.Data["target"] != "pm:bob" {
		t.Fatalf("toOptions mismatch: %+v", opts)
	}
	cat := d.toCategory(messageCategory())
	if cat.ID != "message" || !cat.HasReplyField || len(cat.Actions) != 2 || cat.Actions[0].ID != notifyActionReply {
		t.Fatalf("toCategory mismatch: %+v", cat)
	}
}

// notificationFixture returns a test notification used by TestToOptionsAndCategory.
func notificationFixture() notification.Notification {
	return notification.Notification{
		ID:         "id1",
		Title:      "PM from bob",
		Body:       "hi",
		CategoryID: notifyCategoryMessage,
		Data:       map[string]any{"networkId": "7", "target": "pm:bob", "kind": "pm"},
	}
}
