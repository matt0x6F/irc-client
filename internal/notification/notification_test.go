package notification

import "testing"

type fakeDelivery struct {
	withActions []Notification
	authResult  bool
	authErr     error
}

func (f *fakeDelivery) Send(n Notification) error { return nil }
func (f *fakeDelivery) SendWithActions(n Notification) error {
	f.withActions = append(f.withActions, n)
	return nil
}
func (f *fakeDelivery) RegisterCategory(c Category) error   { return nil }
func (f *fakeDelivery) RequestAuthorization() (bool, error) { return f.authResult, f.authErr }
func (f *fakeDelivery) CheckAuthorization() (bool, error)   { return f.authResult, f.authErr }

func TestShouldNotify_prefsMatrix(t *testing.T) {
	cases := []struct {
		name      string
		prefs     Prefs
		focused   bool
		eventType string
		want      bool
	}{
		{"disabled blocks all", Prefs{Enabled: false, PrivateMessages: true}, false, "message.received", false},
		{"unfocused-only suppresses when focused", Prefs{Enabled: true, PrivateMessages: true, OnlyWhenUnfocused: true}, true, "message.received", false},
		{"unfocused-only allows when blurred", Prefs{Enabled: true, PrivateMessages: true, OnlyWhenUnfocused: true}, false, "message.received", true},
		{"focused allowed when toggle off", Prefs{Enabled: true, Mentions: true, OnlyWhenUnfocused: false}, true, "message.received", true},
		{"message needs a message category enabled", Prefs{Enabled: true, ConnectionLost: true}, false, "message.received", false},
		{"connection gated by its toggle", Prefs{Enabled: true, ConnectionLost: false}, false, "connection.lost", false},
		{"connection allowed", Prefs{Enabled: true, ConnectionLost: true}, false, "connection.lost", true},
		{"unknown event never notifies", Prefs{Enabled: true, PrivateMessages: true}, false, "user.joined", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := NewNotifier()
			n.SetPrefs(tc.prefs)
			n.SetFocused(tc.focused)
			if got := n.ShouldNotify(tc.eventType, nil); got != tc.want {
				t.Fatalf("ShouldNotify(%q) = %v, want %v", tc.eventType, got, tc.want)
			}
		})
	}
}

func TestNotifier_Send_usesDelivery(t *testing.T) {
	n := NewNotifier()
	fd := &fakeDelivery{}
	n.SetDelivery(fd)
	n.Send(Notification{ID: "1", Title: "PM from bob", CategoryID: "message"})
	if len(fd.withActions) != 1 || fd.withActions[0].Title != "PM from bob" {
		t.Fatalf("expected SendWithActions to receive the notification, got %+v", fd.withActions)
	}
}

func TestNotifier_Send_nilDeliveryIsNoop(t *testing.T) {
	NewNotifier().Send(Notification{ID: "1", Title: "x"}) // must not panic
}

func TestNotifier_RequestAuthorization_nilDelivery(t *testing.T) {
	ok, err := NewNotifier().RequestAuthorization()
	if ok || err != nil {
		t.Fatalf("nil delivery should report (false, nil), got (%v, %v)", ok, err)
	}
}
