package storage

import "testing"

func TestLinkPreviewCacheRoundTrip(t *testing.T) {
	s := newTestStorage(t) // same helper the other _test.go files use

	if err := s.UpsertLinkPreview(CachedPreview{
		URL: "https://x.example/a", Status: "ok", Title: "A", FetchedAt: 1000,
	}); err != nil {
		t.Fatal(err)
	}

	got, ok, err := s.GetLinkPreview("https://x.example/a", 1100, 600) // age 100 < ttl 600
	if err != nil || !ok {
		t.Fatalf("expected hit, got ok=%v err=%v", ok, err)
	}
	if got.Title != "A" {
		t.Errorf("Title = %q", got.Title)
	}

	_, ok, _ = s.GetLinkPreview("https://x.example/a", 2000, 600) // age 1000 > ttl 600
	if ok {
		t.Error("expected miss for expired row")
	}
}

func TestPruneLinkPreviews(t *testing.T) {
	s := newTestStorage(t)

	// Insert 5 rows with increasing fetched_at values.
	for i := int64(1); i <= 5; i++ {
		if err := s.UpsertLinkPreview(CachedPreview{
			URL:       "https://x.example/" + string(rune('a'+i-1)),
			Status:    "ok",
			FetchedAt: i * 1000,
		}); err != nil {
			t.Fatalf("UpsertLinkPreview %d: %v", i, err)
		}
	}

	// Prune to a cap of 3 (keeps the 3 most-recently-fetched rows).
	if err := s.PruneLinkPreviews(3); err != nil {
		t.Fatalf("PruneLinkPreviews: %v", err)
	}

	// The 2 oldest rows (fetched_at 1000, 2000) should be gone; the 3 newest remain.
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://x.example/a", false}, // fetched_at 1000 — pruned
		{"https://x.example/b", false}, // fetched_at 2000 — pruned
		{"https://x.example/c", true},  // fetched_at 3000 — kept
		{"https://x.example/d", true},  // fetched_at 4000 — kept
		{"https://x.example/e", true},  // fetched_at 5000 — kept
	} {
		_, ok, err := s.GetLinkPreview(tc.url, 99999, 999999)
		if err != nil {
			t.Fatalf("GetLinkPreview(%q): %v", tc.url, err)
		}
		if ok != tc.want {
			t.Errorf("GetLinkPreview(%q) ok=%v, want %v", tc.url, ok, tc.want)
		}
	}
}
