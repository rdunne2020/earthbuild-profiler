package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EarthBuild/earthbuild-profiler/internal/profile"
)

func TestPersistentUpsertHistoryAndDelete(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state", "runs.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open("../../testdata/demo.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p, err := profile.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.Save(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Logs["test-run"][0].Data = []byte("updated\n")
	second, err := db.Save(p)
	if err != nil {
		t.Fatal(err)
	}
	if !first.ImportedAt.Equal(second.ImportedAt) {
		t.Fatal("reimport changed original import date")
	}
	p.Manifest.BuildID = "newer-run"
	p.Manifest.StartedAt += 100
	if _, err := db.Save(p); err != nil {
		t.Fatal(err)
	}
	// Reopening proves state is on disk, and no earlier operation retains a lock.
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := reopened.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].ID != "newer-run" {
		t.Fatalf("history order = %+v", runs)
	}
	saved, err := reopened.Load("demo-failed-run")
	if err != nil {
		t.Fatal(err)
	}
	if string(saved.Logs["test-run"][0].Data) != "updated\n" {
		t.Fatal("updated logs not persisted")
	}
	if string(saved.Manifest.Extra["gitAuthor"]) != `"Demo Author"` {
		t.Fatalf("unknown metadata not persisted: %q", saved.Manifest.Extra)
	}
	if err := reopened.Delete("demo-failed-run"); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Load("demo-failed-run"); err == nil {
		t.Fatal("deleted run still loads")
	}
	runs, err = reopened.List()
	if err != nil || len(runs) != 1 {
		t.Fatalf("delete did not update summaries: %v, %v", runs, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("DB mode = %o, want 600", info.Mode().Perm())
	}
}
