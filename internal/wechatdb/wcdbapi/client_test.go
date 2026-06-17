package wcdbapi

import (
	"path/filepath"
	"testing"
)

func TestResolveDBPathBareFileUsesGroupSubdir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "db_storage")
	client := &Client{dataDir: dataDir}

	got, err := client.resolveDBPath("session", "session.db")
	if err != nil {
		t.Fatalf("resolveDBPath returned error: %v", err)
	}
	want := filepath.Join(dataDir, "session", "session.db")
	if got != want {
		t.Fatalf("resolveDBPath() = %q, want %q", got, want)
	}
}

func TestResolveDBPathPreservesRelativeSubdir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "db_storage")
	client := &Client{dataDir: dataDir}

	got, err := client.resolveDBPath("session", "session/session.db")
	if err != nil {
		t.Fatalf("resolveDBPath returned error: %v", err)
	}
	want := filepath.Join(dataDir, "session", "session.db")
	if got != want {
		t.Fatalf("resolveDBPath() = %q, want %q", got, want)
	}
}

func TestResolveDBPathDefaultKind(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "db_storage")
	client := &Client{dataDir: dataDir}

	got, err := client.resolveDBPath("contact", "")
	if err != nil {
		t.Fatalf("resolveDBPath returned error: %v", err)
	}
	want := filepath.Join(dataDir, "contact", "contact.db")
	if got != want {
		t.Fatalf("resolveDBPath() = %q, want %q", got, want)
	}
}
