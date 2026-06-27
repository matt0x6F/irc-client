package script

import "testing"

func TestParseManifest(t *testing.T) {
	m := parseManifest("testdata/withmanifest")
	if m.Name != "Greeter Deluxe" {
		t.Fatalf("Name = %q; want Greeter Deluxe", m.Name)
	}
	if m.Description != "Greets people who say hello." {
		t.Fatalf("Description = %q", m.Description)
	}
	if len(m.Permissions) != 2 || m.Permissions[0] != "storage" || m.Permissions[1] != "network" {
		t.Fatalf("Permissions = %v; want [storage network]", m.Permissions)
	}
}

func TestParseManifestAbsent(t *testing.T) {
	m := parseManifest("testdata/greeter") // no header
	if m.Name != "" || m.Description != "" || len(m.Permissions) != 0 {
		t.Fatalf("expected empty manifest for greeter, got %+v", m)
	}
}
