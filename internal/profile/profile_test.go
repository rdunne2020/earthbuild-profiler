package profile

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T) *Profile {
	t.Helper()
	data, err := os.ReadFile("../../testdata/demo.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNativeExportRoundTrip(t *testing.T) {
	t.Parallel()
	p := fixture(t)
	want := Timestamp(1791556200000000000)
	if p.Manifest.StartedAt != want {
		t.Fatalf("timestamp = %d, want %d", p.Manifest.StartedAt, want)
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	q, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if q.Manifest.StartedAt != want {
		t.Fatalf("round-trip lost timestamp precision: %d", q.Manifest.StartedAt)
	}
	if string(q.Manifest.Extra["gitAuthor"]) != `"Demo Author"` {
		t.Fatalf("round-trip lost unknown manifest fields: %q", q.Manifest.Extra)
	}
	if q.CommandDurations["build-deps"] == nil || *q.CommandDurations["build-deps"] != 0 {
		t.Fatal("cached duration lost")
	}
}

func TestCommandAndSubtreeLogs(t *testing.T) {
	t.Parallel()
	p := fixture(t)
	stdout := string(p.LogData("test-run", 1))
	if !strings.Contains(stdout, "TestResponse") || strings.Contains(stdout, "Compiling") || strings.Contains(stdout, "exit code") {
		t.Fatalf("wrong stdout scope: %q", stdout)
	}
	if got := string(p.LogData("test-run", 2)); got != "FAIL app/service\nexit code 1\n" {
		t.Fatalf("stderr = %q", got)
	}
	if strings.Contains(string(p.LogData("test-run", 0)), "memory") {
		t.Fatal("statistics mixed into text output")
	}
	if got := string(p.LogData("_generic:default", 0)); got != "Starting local build\n" {
		t.Fatalf("generic output = %q", got)
	}
	own := string(p.TargetLogData("all", false, 0))
	if own != "" {
		t.Fatalf("root has no own logs: %q", own)
	}
	subtree := string(p.TargetLogData("all", true, 0))
	if strings.Count(subtree, "Compiling app") != 1 || !strings.Contains(subtree, "TestResponse") {
		t.Fatalf("shared dependency output duplicated or absent: %q", subtree)
	}
	p.Manifest.Targets["build"] = Target{DependsOn: []string{"all"}}
	if got := len(p.Descendants("all")); got != 4 {
		t.Fatalf("cyclic graph traversal = %d targets, want 4", got)
	}
}

func TestRejectBadInput(t *testing.T) {
	t.Parallel()
	for name, input := range map[string]string{
		"version":              `{"version":2}`,
		"empty id":             `{"version":1,"manifest":{}}`,
		"missing root":         `{"version":1,"manifest":{"buildId":"a","mainTargetId":"missing"}}`,
		"missing dependency":   `{"version":1,"manifest":{"buildId":"a","targets":{"a":{"dependsOn":["b"]}}}}`,
		"fractional timestamp": `{"version":1,"manifest":{"buildId":"a","startedAtUnixNanos":1.5}}`,
		"trailing object":      `{"version":1,"manifest":{"buildId":"a"}} {}`,
		"bad base64":           `{"version":1,"manifest":{"buildId":"a"},"logs":{"a":[{"data":"@"}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Decode(strings.NewReader(input)); err == nil {
				t.Fatal("want invalid-profile error")
			}
		})
	}
}

func TestDurationFormatting(t *testing.T) {
	t.Parallel()
	if got := Duration(nil); got != "unknown" {
		t.Fatalf("missing duration = %q", got)
	}
	for _, tc := range []struct {
		n    uint64
		want string
	}{{0, "0s"}, {17, "17ns"}, {1000000, "1ms"}, {1500000000, "1.5s"}} {
		if got := Duration(&tc.n); got != tc.want {
			t.Fatalf("Duration(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
