package storage

import (
	"testing"
	"time"
)

func TestScriptStateDisableAndList(t *testing.T) {
	s := newTestStorage(t)

	// A brand-new DB has no disabled scripts.
	disabled, err := s.DisabledScripts()
	if err != nil {
		t.Fatalf("DisabledScripts (empty): %v", err)
	}
	if len(disabled) != 0 {
		t.Fatalf("expected 0 disabled scripts, got %d", len(disabled))
	}

	// Disable a script — it should now appear in the disabled set.
	if err := s.SetScriptEnabled("my-script", false); err != nil {
		t.Fatalf("SetScriptEnabled(false): %v", err)
	}

	disabled, err = s.DisabledScripts()
	if err != nil {
		t.Fatalf("DisabledScripts after disable: %v", err)
	}
	if !disabled["my-script"] {
		t.Errorf("expected 'my-script' in disabled set, got %v", disabled)
	}

	// Re-enable the script — it must disappear from the disabled set.
	if err := s.SetScriptEnabled("my-script", true); err != nil {
		t.Fatalf("SetScriptEnabled(true): %v", err)
	}

	disabled, err = s.DisabledScripts()
	if err != nil {
		t.Fatalf("DisabledScripts after re-enable: %v", err)
	}
	if disabled["my-script"] {
		t.Errorf("expected 'my-script' not in disabled set after re-enable, got %v", disabled)
	}
}

func TestScriptStateMultiple(t *testing.T) {
	s := newTestStorage(t)

	// Disable two scripts, enable one back; only the still-disabled one should appear.
	if err := s.SetScriptEnabled("alpha", false); err != nil {
		t.Fatalf("SetScriptEnabled(alpha,false): %v", err)
	}
	if err := s.SetScriptEnabled("beta", false); err != nil {
		t.Fatalf("SetScriptEnabled(beta,false): %v", err)
	}
	if err := s.SetScriptEnabled("alpha", true); err != nil {
		t.Fatalf("SetScriptEnabled(alpha,true): %v", err)
	}

	disabled, err := s.DisabledScripts()
	if err != nil {
		t.Fatalf("DisabledScripts: %v", err)
	}
	if disabled["alpha"] {
		t.Error("expected 'alpha' to be enabled (not in disabled set)")
	}
	if !disabled["beta"] {
		t.Error("expected 'beta' to remain in disabled set")
	}
}

func TestScriptStatePersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/script_state.db"

	// Session 1: disable a script.
	s, err := NewStorage(dbPath, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	if err := s.SetScriptEnabled("persistent-script", false); err != nil {
		t.Fatalf("SetScriptEnabled: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Session 2: reopen and verify the flag survived.
	s2, err := NewStorage(dbPath, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage (reopen): %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })

	disabled, err := s2.DisabledScripts()
	if err != nil {
		t.Fatalf("DisabledScripts after reopen: %v", err)
	}
	if !disabled["persistent-script"] {
		t.Errorf("expected 'persistent-script' still disabled after reopen, got %v", disabled)
	}
}
