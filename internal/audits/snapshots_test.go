package audits

import "testing"

func TestDiffSnapshotsReportsAddedRemovedAndChanged(t *testing.T) {
	from := snapshotContent{
		Entities: []snapshotEntity{
			{ID: "1", Type: "Brand", CanonicalName: "Acme", Status: "proposed", Aliases: []string{"Acme GEO"}},
			{ID: "2", Type: "Product", CanonicalName: "Acme Cloud", Status: "confirmed", Aliases: []string{}},
		},
		Relations: []snapshotRelation{{Subject: "Acme", Predicate: "brand", Object: "Acme Cloud", Status: "proposed"}},
	}
	to := snapshotContent{
		Entities: []snapshotEntity{
			{ID: "1", Type: "Brand", CanonicalName: "Acme", Status: "confirmed", Aliases: []string{"Acme GEO", "Acme Inc"}},
			{ID: "3", Type: "Organization", CanonicalName: "Northwind", Status: "proposed", Aliases: []string{}},
		},
		Relations: []snapshotRelation{{Subject: "Acme", Predicate: "manufacturer", Object: "Northwind", Status: "proposed"}},
	}
	entities, relations := diffSnapshots(from, to)
	if len(entities.Added) != 1 || entities.Added[0]["canonicalName"] != "Northwind" {
		t.Fatalf("unexpected added entities: %+v", entities.Added)
	}
	if len(entities.Removed) != 1 || entities.Removed[0]["canonicalName"] != "Acme Cloud" {
		t.Fatalf("unexpected removed entities: %+v", entities.Removed)
	}
	if len(entities.Changed) != 1 {
		t.Fatalf("unexpected changed entities: %+v", entities.Changed)
	}
	fields, ok := entities.Changed[0]["fields"].(map[string]any)
	if !ok {
		t.Fatalf("changed entry lacks fields: %+v", entities.Changed[0])
	}
	if _, ok := fields["status"]; !ok {
		t.Fatalf("status change missing: %+v", fields)
	}
	if _, ok := fields["aliases"]; !ok {
		t.Fatalf("alias change missing: %+v", fields)
	}
	if len(relations.Added) != 1 || len(relations.Removed) != 1 {
		t.Fatalf("unexpected relation diff: added=%v removed=%v", relations.Added, relations.Removed)
	}
}

func TestDiffSnapshotsIsStableAndSorted(t *testing.T) {
	from := snapshotContent{}
	to := snapshotContent{Entities: []snapshotEntity{
		{Type: "Brand", CanonicalName: "Zeta"},
		{Type: "Brand", CanonicalName: "Alpha"},
	}}
	entities, _ := diffSnapshots(from, to)
	if len(entities.Added) != 2 || entities.Added[0]["canonicalName"] != "Alpha" {
		t.Fatalf("added entities not sorted: %+v", entities.Added)
	}
	if len(entities.Removed) != 0 || len(entities.Changed) != 0 {
		t.Fatalf("unexpected changes: %+v", entities)
	}
}

func TestDiffSnapshotsTreatsNameCaseAsSameEntity(t *testing.T) {
	from := snapshotContent{Entities: []snapshotEntity{{Type: "brand", CanonicalName: "acme", Status: "proposed"}}}
	to := snapshotContent{Entities: []snapshotEntity{{Type: "Brand", CanonicalName: "Acme", Status: "proposed"}}}
	entities, _ := diffSnapshots(from, to)
	if len(entities.Added) != 0 || len(entities.Removed) != 0 || len(entities.Changed) != 0 {
		t.Fatalf("expected no diff, got %+v", entities)
	}
}
