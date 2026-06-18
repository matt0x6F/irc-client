package storage

import "testing"

func TestSTSPolicyRoundTrip(t *testing.T) {
	s := newTestStorage(t)
	const now = int64(1_000_000)

	// Absent host -> not found, no error.
	if _, ok, err := s.GetSTSPolicy("irc.example.com", now); err != nil || ok {
		t.Fatalf("GetSTSPolicy(absent) = ok %v, err %v; want ok=false err=nil", ok, err)
	}

	// Upsert an active policy expiring in the future.
	if err := s.UpsertSTSPolicy("irc.example.com", 6697, now+3600); err != nil {
		t.Fatalf("UpsertSTSPolicy: %v", err)
	}
	pol, ok, err := s.GetSTSPolicy("irc.example.com", now)
	if err != nil || !ok {
		t.Fatalf("GetSTSPolicy(active) = ok %v, err %v; want ok=true", ok, err)
	}
	if pol.Port != 6697 || pol.Hostname != "irc.example.com" || pol.ExpiresAt != now+3600 {
		t.Fatalf("GetSTSPolicy returned %+v", pol)
	}

	// Refresh (upsert) changes port + expiry in place.
	if err := s.UpsertSTSPolicy("irc.example.com", 7000, now+7200); err != nil {
		t.Fatalf("UpsertSTSPolicy refresh: %v", err)
	}
	pol, _, _ = s.GetSTSPolicy("irc.example.com", now)
	if pol.Port != 7000 || pol.ExpiresAt != now+7200 {
		t.Fatalf("after refresh got %+v", pol)
	}

	// A policy is treated as absent once expired (boundary: expires_at <= now).
	if _, ok, _ := s.GetSTSPolicy("irc.example.com", now+7200); ok {
		t.Fatal("expired policy (now == expiry) should not be active")
	}
	if _, ok, _ := s.GetSTSPolicy("irc.example.com", now+99999); ok {
		t.Fatal("expired policy should not be active")
	}

	// GetSTSPolicies returns stored rows regardless of expiry (for the UI).
	if all, err := s.GetSTSPolicies(); err != nil || len(all) != 1 {
		t.Fatalf("GetSTSPolicies = %v (err %v); want 1 row", all, err)
	}

	// Delete removes it.
	if err := s.DeleteSTSPolicy("irc.example.com"); err != nil {
		t.Fatalf("DeleteSTSPolicy: %v", err)
	}
	if all, _ := s.GetSTSPolicies(); len(all) != 0 {
		t.Fatalf("after delete GetSTSPolicies returned %d rows; want 0", len(all))
	}
}
