package syms

import (
	"testing"

	"github.com/traefik/yaegi/interp"
)

// A script that only references the cascade type in a signature must type-check
// when the cascade symbols are injected via Table() — proving the full-path
// import "github.com/matt0x6f/irc-client/cascade" resolves.
func TestCascadeSymbolsResolveImport(t *testing.T) {
	i := interp.New(interp.Options{Unrestricted: false})
	if err := i.Use(Table()); err != nil {
		t.Fatalf("Use(Table()): %v", err)
	}
	src := `package main
import "github.com/matt0x6f/irc-client/cascade"
func OnText(e cascade.TextEvent) { _ = e.Message }
`
	if _, err := i.Eval(src); err != nil {
		t.Fatalf("Eval referencing cascade.TextEvent via import \"cascade\" failed: %v", err)
	}
}

func TestCascadeV12SurfaceResolves(t *testing.T) {
	i := interp.New(interp.Options{Unrestricted: false})
	if err := i.Use(Table()); err != nil {
		t.Fatalf("Use(Table()): %v", err)
	}
	src := `package main
import "github.com/matt0x6f/irc-client/cascade"
func Setup(c *cascade.Client) {
    n := c.Network("Libera")
    _ = n.Name(); _ = n.IsConnected(); _ = n.Nick(); _ = n.IsMe("Matt")
    _ = n.Self().Status(); _ = n.User("alice").Known(); _ = n.User("alice").IsAway()
    n.Notice("alice", "hi"); n.Action("#go", "waves"); n.Join("#go")
    n.JoinWithKey("#private", "key"); n.Part("#go", "bye"); n.ChangeNick("Matt2")
    n.SetAway("lunch"); n.ClearAway()
}
func OnQuit(cascade.QuitEvent) {}
func OnKick(cascade.KickEvent) {}
func OnNick(cascade.NickEvent) {}
func OnUserStatus(cascade.UserStatusEvent) {}
`
	if _, err := i.Eval(src); err != nil {
		t.Fatalf("Eval v1.2 surface: %v", err)
	}
}
