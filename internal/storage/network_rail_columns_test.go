package storage

import "testing"

func TestMigrateNetworkRailColumns(t *testing.T) {
	s := newTestStorage(t) // same helper other storage tests use (see database_test.go)

	for _, col := range []string{"color", "icon_path", "sort_order"} {
		var n int
		if err := s.db.Get(&n,
			"SELECT COUNT(*) FROM pragma_table_info('networks') WHERE name=?", col); err != nil {
			t.Fatalf("pragma for %s: %v", col, err)
		}
		if n != 1 {
			t.Fatalf("expected column %s to exist, got count %d", col, n)
		}
	}

	// sort_order backfills to id for a pre-existing row.
	id, err := s.db.Exec(`INSERT INTO networks
		(name,address,port,tls,nickname,username,realname,sort_order)
		VALUES ('n','h',6667,0,'nk','u','r', 0)`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	nid, _ := id.LastInsertId()
	// Re-run migration; backfill only touches rows with sort_order=0.
	if err := migrateNetworkRailColumns(s.db); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	var so int64
	if err := s.db.Get(&so, "SELECT sort_order FROM networks WHERE id=?", nid); err != nil {
		t.Fatalf("read sort_order: %v", err)
	}
	if so != nid {
		t.Fatalf("expected sort_order backfilled to id %d, got %d", nid, so)
	}
}
