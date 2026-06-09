package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/liam-od/trawl/internal/job"
)

// writeFile creates a file with the given content under dir, making parents.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSortMovesFileAndCreatesParents(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	library := filepath.Join(root, "library")
	writeFile(t, filepath.Join(inbox, "Example.Movie.2008.mkv"), "movie")

	moves := []job.Move{{
		Src:  "Example.Movie.2008.mkv",
		Dest: "Movies/Example Movie (2008)/Example.Movie.2008.mkv",
	}}
	results := Sort(context.Background(), inbox, library, moves)

	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("results = %+v", results)
	}
	dst := filepath.Join(library, "Movies/Example Movie (2008)/Example.Movie.2008.mkv")
	if got, err := os.ReadFile(dst); err != nil || string(got) != "movie" {
		t.Errorf("dest content = %q, err = %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(inbox, "Example.Movie.2008.mkv")); !os.IsNotExist(err) {
		t.Errorf("source should be gone after move, stat err = %v", err)
	}
}

func TestSortMovesDirectory(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	library := filepath.Join(root, "library")
	writeFile(t, filepath.Join(inbox, "Release.Dir", "episode.mkv"), "ep")

	moves := []job.Move{{Src: "Release.Dir", Dest: "Movies/Film (1983)/Release.Dir"}}
	results := Sort(context.Background(), inbox, library, moves)

	if results[0].Err != nil {
		t.Fatalf("move failed: %v", results[0].Err)
	}
	dst := filepath.Join(library, "Movies/Film (1983)/Release.Dir/episode.mkv")
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("moved dir file missing: %v", err)
	}
}

func TestSortRefusesToOverwrite(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	library := filepath.Join(root, "library")
	writeFile(t, filepath.Join(inbox, "a.mkv"), "new")
	writeFile(t, filepath.Join(library, "Movies/A/a.mkv"), "existing")

	moves := []job.Move{{Src: "a.mkv", Dest: "Movies/A/a.mkv"}}
	results := Sort(context.Background(), inbox, library, moves)

	if results[0].Err == nil {
		t.Fatal("expected an error when destination exists")
	}
	// The existing file and the source must both be left intact.
	if got, _ := os.ReadFile(filepath.Join(library, "Movies/A/a.mkv")); string(got) != "existing" {
		t.Errorf("existing destination was clobbered: %q", got)
	}
	if _, err := os.Stat(filepath.Join(inbox, "a.mkv")); err != nil {
		t.Errorf("source should survive a refused move: %v", err)
	}
}

func TestSortContinuesPastFailure(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	library := filepath.Join(root, "library")
	writeFile(t, filepath.Join(inbox, "good.mkv"), "g")
	// "missing.mkv" is never created, so its move fails.

	moves := []job.Move{
		{Src: "missing.mkv", Dest: "Movies/M/missing.mkv"},
		{Src: "good.mkv", Dest: "Movies/G/good.mkv"},
	}
	results := Sort(context.Background(), inbox, library, moves)

	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Error("first (missing) move should have failed")
	}
	if results[1].Err != nil {
		t.Errorf("second (good) move should have succeeded: %v", results[1].Err)
	}
	moved, failed := Summary(results)
	if moved != 1 || failed != 1 {
		t.Errorf("Summary = %d moved, %d failed; want 1, 1", moved, failed)
	}
}

func TestSortCancelledContext(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "inbox")
	library := filepath.Join(root, "library")
	writeFile(t, filepath.Join(inbox, "a.mkv"), "a")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results := Sort(ctx, inbox, library, []job.Move{{Src: "a.mkv", Dest: "Movies/A/a.mkv"}})
	if results[0].Err == nil {
		t.Error("a cancelled context should fail the move")
	}
	if _, err := os.Stat(filepath.Join(inbox, "a.mkv")); err != nil {
		t.Errorf("source should be untouched after a cancelled run: %v", err)
	}
}
