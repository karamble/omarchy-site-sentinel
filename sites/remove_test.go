package sites

import "testing"

// Remove's return value is what the whole removal path keys off: the HTTP
// handler forgets the monitor state only when it is true, and the CLI turns
// false into "no site matched". It had no test.
func TestRemoveReportsWhetherItRemoved(t *testing.T) {
	s := &Store{Sites: []Site{
		{ID: "a", URL: "https://a.example"},
		{ID: "b", URL: "https://b.example"},
	}}

	if !s.Remove("a") {
		t.Fatal("want true removing a site that is there")
	}
	if len(s.Sites) != 1 || s.Sites[0].ID != "b" {
		t.Fatalf("want only b left, got %+v", s.Sites)
	}

	// The second attempt is the case that produced "no site matched": the id
	// was gone from the store while its monitor state was not.
	if s.Remove("a") {
		t.Fatal("want false removing the same site twice")
	}
	if len(s.Sites) != 1 {
		t.Fatalf("want b untouched, got %+v", s.Sites)
	}
}

func TestRemoveOnAnEmptyStoreIsFalse(t *testing.T) {
	s := &Store{}

	if s.Remove("anything") {
		t.Fatal("want false on an empty store")
	}
}

// Find is what the snapshot filter and the row builder test membership with.
func TestFindReportsMembership(t *testing.T) {
	s := &Store{Sites: []Site{{ID: "a", URL: "https://a.example"}}}

	if _, ok := s.Find("a"); !ok {
		t.Fatal("want a found")
	}
	if _, ok := s.Find("gone"); ok {
		t.Fatal("want gone not found")
	}
}
