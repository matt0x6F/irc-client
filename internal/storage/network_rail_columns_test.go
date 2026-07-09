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

func TestUpdateNetworkSortOrderReorders(t *testing.T) {
	s := newTestStorage(t)
	var ids []int64
	for _, name := range []string{"a", "b", "c"} {
		r, err := s.db.Exec(`INSERT INTO networks
			(name,address,port,tls,nickname,username,realname,sort_order)
			VALUES (?, 'h',6667,0,'nk','u','r',0)`, name)
		if err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
		id, _ := r.LastInsertId()
		ids = append(ids, id)
	}
	// Reverse order: c,b,a -> sort_order 1,2,3
	rev := []int64{ids[2], ids[1], ids[0]}
	for i, id := range rev {
		if err := s.UpdateNetworkSortOrder(id, int64(i+1)); err != nil {
			t.Fatalf("update: %v", err)
		}
	}
	var names []string
	if err := s.db.Select(&names, "SELECT name FROM networks ORDER BY sort_order, id"); err != nil {
		t.Fatalf("select: %v", err)
	}
	if got := names; len(got) != 3 || got[0] != "c" || got[1] != "b" || got[2] != "a" {
		t.Fatalf("expected [c b a], got %v", got)
	}
}
