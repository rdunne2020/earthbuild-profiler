package tui

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/EarthBuild/earthbuild-profiler/internal/profile"
	"github.com/EarthBuild/earthbuild-profiler/internal/store"
	"github.com/charmbracelet/x/ansi"
)

func testModel(t *testing.T) *Model {
	t.Helper()
	data, err := os.ReadFile("../../testdata/demo.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := profile.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	m := New(nil, []store.Summary{{ID: p.Manifest.BuildID, Target: p.TargetName(), Status: p.Manifest.Status, Duration: p.RunDuration}}, "")
	m.profile = p
	m.expanded = map[string]bool{"all": true, "build": true, "test": true, "package": true}
	m.rows = treeRows(p, m.expanded, false)
	m.refreshLogs()
	return m
}

func key(m *Model, text string) {
	msg := tea.KeyPressMsg{Code: []rune(text)[0], Text: text}
	m.Update(msg)
}

func TestSharedTreeAndSlowestRanking(t *testing.T) {
	t.Parallel()
	m := testModel(t)
	shared, concrete := 0, 0
	for _, r := range m.rows {
		if r.target && r.id == "build" {
			if r.shared {
				shared++
			} else {
				concrete++
			}
		}
	}
	if shared != 2 || concrete != 1 {
		t.Fatalf("build instances: %d concrete, %d shared", concrete, shared)
	}
	key(m, "2")
	for i, r := range m.rows {
		if r.target && r.id == "build" && r.shared {
			m.cursor = i
			break
		}
	}
	m.expand(true)
	if m.rows[m.cursor].shared || m.rows[m.cursor].id != "build" {
		t.Fatal("shared reference did not jump to actual target")
	}
	key(m, "s")
	if len(m.rows) != len(m.profile.Manifest.Commands) || m.rows[0].id != "all-test" {
		t.Fatalf("wrong slowest-step ranking: %+v", m.rows)
	}
	m.profile.CommandDurations["test-run"] = nil
	m.rows = treeRows(m.profile, m.expanded, true)
	if m.rows[len(m.rows)-1].id != "test-run" {
		t.Fatal("unknown duration should sort last")
	}
}

func TestLogScopeSearchAndStreamSwitch(t *testing.T) {
	t.Parallel()
	m := testModel(t)
	for i, r := range m.rows {
		if r.id == "test-run" {
			m.cursor = i
			break
		}
	}
	m.refreshLogs()
	if !strings.Contains(strings.Join(m.logLines, "\n"), "FAIL app") {
		t.Fatal("selected step's stderr missing")
	}
	key(m, "t")
	if strings.Contains(strings.Join(m.logLines, "\n"), "FAIL app") {
		t.Fatal("stderr leaked into stdout view")
	}
	key(m, "t")
	if !strings.Contains(strings.Join(m.logLines, "\n"), "FAIL app") {
		t.Fatal("stderr stream not selected")
	}
	key(m, "t")
	m.query = "test"
	m.findMatch(1, true)
	first := m.match
	m.findMatch(1, false)
	if m.match == first {
		t.Fatal("next search match did not advance when viewport was clamped")
	}
	m.query = "not-present"
	m.findMatch(1, false)
	if m.message != "No matching log lines" {
		t.Fatal("search miss not reported")
	}
	m.cursor = 0
	m.refreshLogs()
	if len(m.logLines) != 0 {
		t.Fatal("root has no own output")
	}
	key(m, "a")
	if strings.Count(strings.Join(m.logLines, "\n"), "Compiling app") != 1 {
		t.Fatal("subtree logs duplicated shared target")
	}
}

func TestRenderDimensionsAndTerminalSafety(t *testing.T) {
	t.Parallel()
	m := testModel(t)
	for i, r := range m.rows {
		if r.id == "test-run" {
			m.cursor = i
			break
		}
	}
	m.profile.Logs["test-run"][0].Data = []byte("\x1b[2J\x1b]52;c;dGVzdA==\ahello\x00 世界\n")
	m.refreshLogs()
	if got := strings.Join(m.logLines, "\n"); strings.ContainsAny(got, "\x1b\a\x00") || !strings.Contains(got, "hello") {
		t.Fatalf("unsafe terminal log data: %q", got)
	}
	for _, size := range [][2]int{{90, 20}, {120, 32}, {180, 48}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View().Content
		lines := strings.Split(view, "\n")
		if len(lines) > size[1] {
			t.Fatalf("%v: rendered %d rows\n%s", size, len(lines), ansi.Strip(view))
		}
		for _, line := range lines {
			if width := ansi.StringWidth(line); width > size[0] {
				t.Fatalf("%v: rendered %d columns", size, width)
			}
		}
		if !strings.Contains(ansi.Strip(view), "hello") {
			t.Fatalf("log output not rendered at %v", size)
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 10})
	if !strings.Contains(m.View().Content, "Resize") {
		t.Fatal("small-screen guidance absent")
	}
}

func TestCycleAndStaleLoads(t *testing.T) {
	t.Parallel()
	m := testModel(t)
	target := m.profile.Manifest.Targets["build"]
	target.DependsOn = []string{"all"}
	m.profile.Manifest.Targets["build"] = target
	rows := treeRows(m.profile, m.expanded, false)
	if len(rows) > 30 {
		t.Fatal("cycle caused unbounded tree")
	}
	old := m.profile
	m.runs = append(m.runs, store.Summary{ID: "other-run"})
	m.runCursor = 1
	m.Update(loadedMsg{id: "demo-failed-run", profile: &profile.Profile{}})
	if m.profile != old {
		t.Fatal("stale asynchronous load replaced selected run")
	}
	if !slices.ContainsFunc(rows, func(r row) bool { return r.id == "_generic:default" }) {
		t.Fatal("generic CLI output missing from tree")
	}
}
