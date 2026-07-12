package script

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matt0x6f/irc-client/cascade"
)

func TestLoadAndDispatchOnText(t *testing.T) {
	s, err := LoadPackage("testdata/greeter")
	if err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}
	if !s.Has("OnText") {
		t.Fatalf("expected OnText handler to be discovered")
	}

	var got []string
	s.DispatchText(cascade.NewTextEvent("bob", "#chan", "!hello world", func(m string) { got = append(got, m) }))

	if len(got) != 1 || got[0] != "Hi bob" {
		t.Fatalf("reply = %v; want [Hi bob]", got)
	}
}

func TestLifecycleHandlersLoadAndDispatch(t *testing.T) {
	dir := t.TempDir()
	src := `package main
import "github.com/matt0x6f/irc-client/cascade"
func OnQuit(e cascade.QuitEvent) { if e.Nick != "alice" { panic("bad quit") } }
func OnKick(e cascade.KickEvent) { e.Reply("kick:"+e.Nick) }
func OnNick(e cascade.NickEvent) { if e.NewNick != "alice2" { panic("bad nick") } }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadPackage(dir)
	if err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}
	for _, name := range []string{"OnQuit", "OnKick", "OnNick"} {
		if !s.Has(name) {
			t.Fatalf("missing handler %s", name)
		}
	}
	s.DispatchQuit(cascade.NewQuitEvent("alice", "gone"))
	var reply string
	s.DispatchKick(cascade.NewKickEvent("alice", "oper", "#go", "rules", func(message string) { reply = message }))
	s.DispatchNick(cascade.NewNickEvent("alice", "alice2"))
	if reply != "kick:alice" {
		t.Fatalf("kick reply = %q", reply)
	}
}

func TestTypeErrorFailsAtLoad(t *testing.T) {
	s, err := LoadPackage("testdata/typeerror")
	if err == nil {
		t.Fatalf("expected load to fail on a type error, got nil error (script=%v)", s)
	}
}

func TestForbiddenImportFailsToLoad(t *testing.T) {
	if _, err := LoadPackage("testdata/forbidden"); err == nil {
		t.Fatalf("expected import \"os\" to fail (not in symbol table); got nil error")
	}
}

func TestMultiFilePackageLoads(t *testing.T) {
	s, err := LoadPackage("testdata/multifile")
	if err != nil {
		t.Fatalf("LoadPackage(multifile): %v", err)
	}
	var got []string
	s.DispatchText(cascade.NewTextEvent("cara", "#chan", "!x", func(m string) { got = append(got, m) }))
	if len(got) != 1 || got[0] != "yo cara" {
		t.Fatalf("reply = %v; want [yo cara]", got)
	}
}

func TestAliasedImportSurvivesMerge(t *testing.T) {
	s, err := LoadPackage("testdata/aliasedimport")
	if err != nil {
		t.Fatalf("LoadPackage(aliasedimport): %v", err)
	}
	var got []string
	s.DispatchText(cascade.NewTextEvent("dee", "#chan", "!x", func(m string) { got = append(got, m) }))
	if len(got) != 1 || got[0] != "aliased dee" {
		t.Fatalf("reply = %v; want [aliased dee]", got)
	}
}

func TestHasSetupAndRunSetup(t *testing.T) {
	s, err := LoadPackage("testdata/withsetup")
	if err != nil {
		t.Fatalf("LoadPackage(withsetup): %v", err)
	}
	if !s.HasSetup() {
		t.Fatalf("expected HasSetup() == true")
	}

	var afterDelay string
	afterSpy := func(delay string, _ func()) { afterDelay = delay }
	c := cascade.NewClient(nil, nil, afterSpy)

	if err := s.RunSetup(c); err != nil {
		t.Fatalf("RunSetup: %v", err)
	}
	if afterDelay != "0s" {
		t.Fatalf("afterSpy delay = %q; want \"0s\"", afterDelay)
	}
}

func TestNoSetupIsNoop(t *testing.T) {
	s, err := LoadPackage("testdata/greeter")
	if err != nil {
		t.Fatalf("LoadPackage(greeter): %v", err)
	}
	if s.HasSetup() {
		t.Fatalf("expected HasSetup() == false for greeter")
	}
	if err := s.RunSetup(nil); err != nil {
		t.Fatalf("RunSetup on script without Setup returned error: %v", err)
	}
}

func TestRunSetupPanicRecovered(t *testing.T) {
	s, err := LoadPackage("testdata/panicsetup")
	if err != nil {
		t.Fatalf("LoadPackage(panicsetup): %v", err)
	}
	if !s.HasSetup() {
		t.Fatalf("expected HasSetup() == true")
	}
	c := cascade.NewClient(nil, nil, nil)
	err = s.RunSetup(c)
	if err == nil {
		t.Fatalf("expected RunSetup to return an error from panicking Setup")
	}
	if !strings.Contains(err.Error(), "Setup panic") {
		t.Fatalf("error %q does not mention Setup panic", err.Error())
	}
}
