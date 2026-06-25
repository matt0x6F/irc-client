package irc

import (
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"
	"github.com/matt0x6f/irc-client/internal/events"
)

// mustParseTagmsg parses a raw IRC line (with tags) into an ircmsg.Message for
// feeding the typing handler directly.
func mustParseTagmsg(t *testing.T, raw string) ircmsg.Message {
	t.Helper()
	m, err := ircmsg.ParseLine(raw)
	if err != nil {
		t.Fatalf("ParseLine(%q): %v", raw, err)
	}
	return m
}

// expectNoTypingEvent fails if any typing event lands on the sink within a short
// grace window (used for the "ignored" cases).
func expectNoTypingEvent(t *testing.T, sink *historyEventSink) {
	t.Helper()
	select {
	case e := <-sink.ch:
		t.Fatalf("expected no typing event, got %+v", e)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestValidTypingState(t *testing.T) {
	for _, s := range []string{"active", "paused", "done"} {
		if !validTypingState(s) {
			t.Errorf("expected %q to be a valid typing state", s)
		}
	}
	for _, s := range []string{"", "ACTIVE", "typing", "bogus", "active "} {
		if validTypingState(s) {
			t.Errorf("expected %q to be rejected", s)
		}
	}
}

func TestHandleTypingTagChannel(t *testing.T) {
	c := newHistoryTestClient(t)
	sink := &historyEventSink{ch: make(chan events.Event, 4)}
	c.eventBus.Subscribe(EventTypingReceived, sink)

	c.handleTypingTag(mustParseTagmsg(t, "@+typing=active :alice!a@h TAGMSG #hist"))

	select {
	case e := <-sink.ch:
		if got, _ := e.Data["target"].(string); got != "#hist" {
			t.Errorf("target: want #hist, got %q", got)
		}
		if got, _ := e.Data["nick"].(string); got != "alice" {
			t.Errorf("nick: want alice, got %q", got)
		}
		if got, _ := e.Data["state"].(string); got != "active" {
			t.Errorf("state: want active, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for typing event")
	}
}

func TestHandleTypingTagPM(t *testing.T) {
	c := newHistoryTestClient(t) // network nick is matt0x6f
	sink := &historyEventSink{ch: make(chan events.Event, 4)}
	c.eventBus.Subscribe(EventTypingReceived, sink)

	// Addressed to us by nick => a PM typing notification from carol.
	c.handleTypingTag(mustParseTagmsg(t, "@+typing=paused :carol!c@h TAGMSG matt0x6f"))

	select {
	case e := <-sink.ch:
		if got, _ := e.Data["target"].(string); got != "carol" {
			t.Errorf("target: want carol (the PM peer), got %q", got)
		}
		if got, _ := e.Data["nick"].(string); got != "carol" {
			t.Errorf("nick: want carol, got %q", got)
		}
		if got, _ := e.Data["state"].(string); got != "paused" {
			t.Errorf("state: want paused, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PM typing event")
	}
}

func TestHandleTypingTagIgnoresUnknownState(t *testing.T) {
	c := newHistoryTestClient(t)
	sink := &historyEventSink{ch: make(chan events.Event, 4)}
	c.eventBus.Subscribe(EventTypingReceived, sink)

	c.handleTypingTag(mustParseTagmsg(t, "@+typing=bogus :alice!a@h TAGMSG #hist"))
	expectNoTypingEvent(t, sink)
}

func TestHandleTypingTagIgnoresMissingTag(t *testing.T) {
	c := newHistoryTestClient(t)
	sink := &historyEventSink{ch: make(chan events.Event, 4)}
	c.eventBus.Subscribe(EventTypingReceived, sink)

	c.handleTypingTag(mustParseTagmsg(t, ":alice!a@h TAGMSG #hist"))
	expectNoTypingEvent(t, sink)
}

func TestHandleTypingTagIgnoresSelfEcho(t *testing.T) {
	c := newHistoryTestClient(t) // network nick is matt0x6f
	sink := &historyEventSink{ch: make(chan events.Event, 4)}
	c.eventBus.Subscribe(EventTypingReceived, sink)

	// echo-message can echo our own TAGMSG back; it must be dropped.
	c.handleTypingTag(mustParseTagmsg(t, "@+typing=active :matt0x6f!m@h TAGMSG #hist"))
	expectNoTypingEvent(t, sink)
}

func TestSendTypingRejectsInvalidState(t *testing.T) {
	c := &IRCClient{enabledCaps: map[string]bool{"message-tags": true}}
	if err := c.SendTyping("#x", "bogus"); err == nil {
		t.Fatal("expected an error for an invalid typing state")
	}
}

func TestSendTypingNoopWithoutMessageTags(t *testing.T) {
	// No message-tags cap => best-effort no-op, no error, no connection touched.
	c := &IRCClient{enabledCaps: map[string]bool{}}
	if err := c.SendTyping("#x", "active"); err != nil {
		t.Fatalf("expected silent no-op without message-tags, got error: %v", err)
	}
}
