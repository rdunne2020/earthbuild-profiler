package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/EarthBuild/earthbuild-profiler/internal/profile"
	"github.com/charmbracelet/x/ansi"
)

var (
	accent     = lipgloss.NewStyle().Foreground(lipgloss.Color("#6ED6C0")).Bold(true)
	muted      = lipgloss.NewStyle().Foreground(lipgloss.Color("#8C98AA"))
	selected   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#284C61"))
	failure    = lipgloss.NewStyle().Foreground(lipgloss.Color("#F28B82"))
	matchStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F5D88A"))
)

// View lays out run history, the build graph, and a command-specific log viewport.
func (m *Model) View() tea.View {
	content := m.render()
	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "EarthBuild Profiler"
	return v
}

func (m *Model) render() string {
	if m.width < 90 || m.height < 20 {
		return fmt.Sprintf("EarthBuild Profiler\nResize the terminal to at least 90 x 20 (currently %d x %d).\nq: quit", m.width, m.height)
	}
	if m.help {
		return m.helpView()
	}
	header := accent.Render("EarthBuild Profiler") + muted.Render("  local run history")
	summary := "Import a profile with: earth-profile import run.json"
	if m.profile != nil {
		summary = fmt.Sprintf("%s  %s  %s  %s", oneLine(m.profile.TargetName()), profile.Duration(m.profile.RunDuration),
			profile.Status(m.profile.Manifest.Status), oneLine(m.profile.Manifest.BuildID))
	} else if m.loading {
		summary = "Loading run..."
	}
	header = ansi.Truncate(header, m.width, "") + "\n" + muted.Render(ansi.Truncate(summary, m.width, "...")) + "\n"
	leftW := 25
	treeW := max(28, (m.width-leftW)*45/100)
	logW := m.width - leftW - treeW
	panelH := m.height - 6
	left := panel("1 Runs", m.runLines(leftW-2, panelH-3), leftW, panelH, m.focus == 0)
	mode := "2 Targets / steps"
	if m.slow {
		mode = "2 Slowest steps"
	}
	middle := panel(mode, m.treeLines(treeW-2, panelH-3), treeW, panelH, m.focus == 1)
	right := panel("3 Logs ["+m.streamName()+"]", m.detailLines(logW-2, panelH-3), logW, panelH, m.focus == 2)
	footer := "tab: focus  enter: expand  s: slowest  t: stream  /: search  ?: help  q: quit"
	status := m.message
	if m.searching {
		status = "Search logs: " + oneLine(m.query) + "_"
	} else if m.query != "" && status == "" {
		status = "Search: " + oneLine(m.query) + "  n/N: next/previous match"
	}
	return header + lipgloss.JoinHorizontal(lipgloss.Top, left, middle, right) + "\n" +
		accent.Render(ansi.Truncate(status, m.width, "...")) + "\n" + muted.Render(ansi.Truncate(footer, m.width, "..."))
}

func panel(title string, lines []string, width, height int, active bool) string {
	color := "#3F4B5C"
	if active {
		color = "#6ED6C0"
	}
	body := []string{accent.Render(ansi.Truncate(title, width-2, "..."))}
	for _, line := range lines {
		body = append(body, ansi.Truncate(line, width-2, "..."))
	}
	for len(body) < height-2 {
		body = append(body, "")
	}
	body = body[:min(len(body), height-2)]
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(color)).
		Width(width).Height(height).Render(strings.Join(body, "\n"))
}

func (m *Model) runLines(width, height int) []string {
	if len(m.runs) == 0 {
		return []string{"No saved runs.", "", "earth-profile import", "  run.json", "", "r: refresh history"}
	}
	visible := max(1, height/3)
	start := windowStart(m.runCursor, len(m.runs), visible)
	var lines []string
	for i := start; i < min(len(m.runs), start+visible); i++ {
		r := m.runs[i]
		name := ansi.Truncate(oneLine(r.Target), width, "...")
		detail := fmt.Sprintf("%s  %s", profile.Duration(r.Duration), profile.Status(r.Status))
		when := r.ImportedAt.Local().Format("Jan 02 15:04")
		if r.StartedAt > 0 && uint64(r.StartedAt) <= 1<<63-1 {
			when = time.Unix(0, int64(r.StartedAt)).Local().Format("Jan 02 15:04")
		}
		if i == m.runCursor {
			name = selected.Width(width).Render(name)
		} else if profile.Status(r.Status) == "FAILURE" {
			name = failure.Render(name)
		}
		lines = append(lines, name, muted.Render(ansi.Truncate(detail, width, "...")), muted.Render(when))
	}
	return lines
}

func (m *Model) treeLines(width, height int) []string {
	if m.loading {
		return []string{"Loading..."}
	}
	if len(m.rows) == 0 {
		return []string{"No recorded steps."}
	}
	start := windowStart(m.cursor, len(m.rows), height)
	var lines []string
	for i := start; i < min(len(m.rows), start+height); i++ {
		r := m.rows[i]
		marker := " "
		if r.target {
			marker = ">"
			if m.expanded[r.id] {
				marker = "v"
			}
			if r.shared {
				marker = "="
			}
		}
		name := strings.Repeat(" ", min(r.depth*2, max(0, width/3))) + marker + " " + oneLine(r.label)
		if r.shared {
			name += " [shared]"
		}
		if r.cached {
			name += " [cache]"
		}
		duration := profile.Duration(r.duration)
		nameW := max(1, width-len(duration)-1)
		line := ansi.Truncate(name, nameW, "...")
		line += strings.Repeat(" ", max(1, width-ansi.StringWidth(line)-len(duration))) + duration
		if i == m.cursor {
			line = selected.Width(width).Render(line)
		} else if profile.Status(r.status) == "FAILURE" {
			line = failure.Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}

func (m *Model) detailLines(width, height int) []string {
	if m.profile == nil || len(m.rows) == 0 {
		return []string{"Select a saved run", "and a target or step."}
	}
	r := m.rows[m.cursor]
	meta := []string{oneLine(r.label), "ID: " + r.id, "Time: " + profile.Duration(r.duration) + "  " + profile.Status(r.status)}
	if r.target {
		t := m.profile.Manifest.Targets[r.id]
		platform := t.FinalPlatform
		if platform == "" {
			platform = t.InitialPlatform
		}
		scope := "own steps"
		if m.subtree {
			scope = "dependency subtree"
		}
		meta = append(meta, "Platform: "+oneLine(platform), fmt.Sprintf("Dependencies: %d", len(t.DependsOn)), "Scope: "+scope+" (a: toggle)")
	} else if c, ok := m.profile.Manifest.Commands[r.id]; ok {
		source := "(source unavailable)"
		if c.Source != nil {
			source = fmt.Sprintf("%s:%d", oneLine(c.Source.File), c.Source.StartLine)
		}
		info := "Platform: " + oneLine(c.Platform)
		if c.Cached {
			info = "Cached step"
		}
		if c.ErrorMessage != "" {
			info = "Error: " + oneLine(c.ErrorMessage)
		}
		meta = append(meta, "Source: "+source, info, fmt.Sprintf("Target: %s  References: %d", oneLine(m.profile.Manifest.Targets[c.TargetID].Name), len(c.DependsOn)))
	} else {
		meta = append(meta, "CLI output", "", "")
	}
	lines := make([]string, 0, height)
	for _, line := range meta {
		lines = append(lines, muted.Render(ansi.Truncate(line, width, "...")))
	}
	lines = append(lines, muted.Render(strings.Repeat("-", width)))
	logH := max(1, height-len(lines)-1)
	if len(m.logLines) == 0 {
		lines = append(lines, muted.Render("No output on this stream."))
	} else {
		for i := m.logOffset; i < min(len(m.logLines), m.logOffset+logH); i++ {
			line := ansi.TruncateLeft(m.logLines[i], m.logX, "")
			line = ansi.Truncate(line, width, "")
			if m.query != "" && strings.Contains(strings.ToLower(m.logLines[i]), strings.ToLower(m.query)) {
				line = matchStyle.Render(line)
			}
			lines = append(lines, line)
		}
	}
	for len(lines) < height-1 {
		lines = append(lines, "")
	}
	lines = append(lines, muted.Render(fmt.Sprintf("lines %d-%d / %d", min(m.logOffset+1, len(m.logLines)), min(m.logOffset+logH, len(m.logLines)), len(m.logLines))))
	return lines
}

func (m *Model) streamName() string {
	switch m.stream {
	case 1:
		return "stdout"
	case 2:
		return "stderr"
	default:
		return "stdout + stderr"
	}
}

func (m *Model) helpView() string {
	text := `Navigation
  tab / shift+tab   Cycle focus: runs, tree, logs
  1 / 2 / 3         Focus a pane directly
  j/k or arrows     Move selection or scroll logs
  PgUp/PgDn         Page through the focused pane
  g/G               First/last item or log line
  h/l               Collapse/expand targets; scroll logs horizontally
  enter / space     Expand target; jump to a shared target; focus step logs
  E / C             Expand/collapse all targets

Inspection
  s                 Toggle dependency tree / slowest-step ranking
  a                 Toggle target's own logs / full dependency subtree
  t                 Cycle stdout + stderr / stdout / stderr
  /                 Search selected logs (case insensitive)
  n / N             Next/previous match, wrapping at the end
  esc               Clear search
  r                 Reload saved run history
  q / ctrl+c        Quit

Durations overlap when work runs in parallel. "unknown" means no complete
recorded interval. Shared references refer to one target instance, not a
second execution. Logs are scoped by instance ID, not by target name.

? or esc closes this help.`
	return panel("EarthBuild Profiler / keys", strings.Split(text, "\n"), m.width, m.height, false)
}
