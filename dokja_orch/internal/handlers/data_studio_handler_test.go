package handlers

import "testing"

func TestDataStudioHandlerCollapsesConnectionsSharingOneDSN(t *testing.T) {
	h := NewDataStudioHandler(t.TempDir(), map[string]string{
		"price_history":  "postgres://u:p@db:5432/dokja",
		"source_journal": "postgres://u:p@db:5432/dokja",
		"legacy_mirror":  "postgres://u:p@old:5432/dokja",
	})

	if len(h.postgresConns) != 2 {
		t.Fatalf("expected 2 connections, got %d: %#v", len(h.postgresConns), h.postgresConns)
	}
	byDSN := map[string]string{}
	for _, conn := range h.postgresConns {
		if conn.ID == "" || conn.DSN == "" {
			t.Fatalf("connection missing id or dsn: %#v", conn)
		}
		byDSN[conn.DSN] = conn.Name
	}
	if name := byDSN["postgres://u:p@db:5432/dokja"]; name != "price_history · source_journal" {
		t.Fatalf("shared DSN should join both names, got %q", name)
	}
	if name := byDSN["postgres://u:p@old:5432/dokja"]; name != "legacy_mirror" {
		t.Fatalf("single DSN keeps its name, got %q", name)
	}
}

func TestDataStudioHandlerOrdersConnectionsForStableIDs(t *testing.T) {
	first := NewDataStudioHandler(t.TempDir(), map[string]string{
		"b": "postgres://u:p@db:5432/dokja",
		"a": "postgres://u:p@db:5432/dokja",
	})
	second := NewDataStudioHandler(t.TempDir(), map[string]string{
		"a": "postgres://u:p@db:5432/dokja",
		"b": "postgres://u:p@db:5432/dokja",
	})
	if len(first.postgresConns) != 1 || len(second.postgresConns) != 1 {
		t.Fatalf("both should collapse to one connection")
	}
	if first.postgresConns[0] != second.postgresConns[0] {
		t.Fatalf("connection ids and names must be stable across builds: %#v vs %#v", first.postgresConns[0], second.postgresConns[0])
	}
}
