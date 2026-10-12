package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestImportListLogsAndExport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db := filepath.Join(dir, "runs.db")
	data, err := os.ReadFile("../../testdata/demo.json")
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(args []string, input []byte) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := run(append([]string{"--db", db}, args...), bytes.NewReader(input), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	if code, _, errout := invoke([]string{"import", "-"}, data); code != 0 {
		t.Fatalf("import failed: %d %s", code, errout)
	}
	if code, out, errout := invoke([]string{"runs"}, nil); code != 0 || !strings.Contains(out, "demo-failed-run") {
		t.Fatalf("runs failed: %d %s %s", code, out, errout)
	}
	if code, out, errout := invoke([]string{"logs", "--stream", "2", "demo-failed-run", "test-run"}, nil); code != 0 || out != "FAIL app/service\nexit code 1\n" {
		t.Fatalf("logs failed: %d %q %s", code, out, errout)
	}
	if code, _, errout := invoke([]string{"export", "demo-failed-run", filepath.Join(dir, "export.json")}, nil); code != 0 {
		t.Fatalf("export failed: %d %s", code, errout)
	}
	exported, err := os.ReadFile(filepath.Join(dir, "export.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(exported, []byte(`"gitAuthor":"Demo Author"`)) {
		t.Fatal("export lost metadata")
	}
	if code, _, errout := invoke([]string{"import"}, exported); code != 0 {
		t.Fatalf("export reimport failed: %d %s", code, errout)
	}
	if code, _, _ := invoke([]string{"import"}, []byte(`{"version":99}`)); code == 0 {
		t.Fatal("invalid import succeeded")
	}
	if code, _, _ := invoke([]string{"logs", "demo-failed-run", "missing-step"}, nil); code == 0 {
		t.Fatal("missing step silently succeeded")
	}
	if code, _, errout := invoke([]string{"delete", "demo-failed-run"}, nil); code != 0 {
		t.Fatalf("delete failed: %d %s", code, errout)
	}
}

func TestCaptureImportsFailedBuildAndPreservesExitCode(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell stand-in for earth")
	}
	dir := t.TempDir()
	fixture, err := filepath.Abs("../../testdata/demo.json")
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(dir, "fake-earth")
	// Arguments passed by the wrapper are --profile FILE +all.
	script := "#!/bin/sh\n[ \"$1\" = '--profile' ] || exit 3\ncp '" + fixture + "' \"$2\"\nprintf 'simulated build output\\n'\nexit 7\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	db := filepath.Join(dir, "runs.db")
	code := run([]string{"--db", db, "--earth", fake, "run", "+all"}, strings.NewReader(""), &stdout, &stderr)
	if code != 7 || !strings.Contains(stderr.String(), "Saved profile demo-failed-run") || !strings.Contains(stdout.String(), "simulated build output") {
		t.Fatalf("capture = %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--db", db, "runs"}, strings.NewReader(""), &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "demo-failed-run") {
		t.Fatal("failed build's profile not saved")
	}
}
