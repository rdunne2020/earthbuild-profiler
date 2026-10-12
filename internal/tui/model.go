// Package tui provides the Bubble Tea run-history, dependency-tree, and log viewer.
package tui

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/EarthBuild/earthbuild-profiler/internal/profile"
	"github.com/EarthBuild/earthbuild-profiler/internal/store"
	"github.com/charmbracelet/x/ansi"
)

type loadedMsg struct {
	id      string
	profile *profile.Profile
	err     error
}
type runsMsg struct {
	runs []store.Summary
	err  error
}

// Model owns all viewer state. Database operations run as Bubble Tea commands.
type Model struct {
	match     int
	db        *store.Store
	runs      []store.Summary
	runCursor int
	profile   *profile.Profile
	rows      []row
	expanded  map[string]bool
	cursor    int
	focus     int
	width     int
	height    int
	slow      bool
	subtree   bool
	stream    int32
	logLines  []string
	logOffset int
	logX      int
	query     string
	searching bool
	loading   bool
	message   string
	help      bool
}

// New creates a viewer, optionally selecting a particular saved run.
func New(db *store.Store, runs []store.Summary, initialID string) *Model {
	m := &Model{db: db, runs: runs, expanded: make(map[string]bool), width: 120, height: 32, match: -1}
	for i, r := range runs {
		if r.ID == initialID {
			m.runCursor = i
			break
		}
	}
	return m
}

// Init loads the selected run without blocking the terminal event loop.
func (m *Model) Init() tea.Cmd { return m.loadRun() }

func (m *Model) loadRun() tea.Cmd {
	if len(m.runs) == 0 {
		return nil
	}
	id := m.runs[m.runCursor].ID
	m.loading = true
	m.profile = nil
	m.rows = nil
	m.logLines = nil
	m.message = ""
	return func() tea.Msg {
		p, err := m.db.Load(id)
		return loadedMsg{id: id, profile: p, err: err}
	}
}

// Update handles navigation, filtering, asynchronous loads, and terminal resizing.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampLogs()
	case loadedMsg:
		if len(m.runs) == 0 || m.runs[m.runCursor].ID != msg.id {
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			m.message = msg.err.Error()
			return m, nil
		}
		m.profile = msg.profile
		m.cursor = 0
		m.expanded = map[string]bool{m.profile.Manifest.MainTargetID: true}
		m.rows = treeRows(m.profile, m.expanded, m.slow)
		m.refreshLogs()
	case runsMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
			return m, nil
		}
		id := ""
		if len(m.runs) > 0 {
			id = m.runs[m.runCursor].ID
		}
		m.runs, m.runCursor = msg.runs, 0
		for i, r := range m.runs {
			if r.ID == id {
				m.runCursor = i
				break
			}
		}
		if len(m.runs) == 0 {
			m.profile, m.rows, m.logLines = nil, nil, nil
			return m, nil
		}
		return m, m.loadRun()
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.searching {
			switch key {
			case "enter":
				m.searching = false
				m.findMatch(1, true)
			case "esc":
				m.searching = false
				m.query = ""
			case "backspace":
				runes := []rune(m.query)
				if len(runes) > 0 {
					m.query = string(runes[:len(runes)-1])
				}
			default:
				m.query += msg.Text
			}
			return m, nil
		}
		if m.help {
			if key == "?" || key == "esc" || key == "q" {
				m.help = false
			}
			return m, nil
		}
		switch key {
		case "q":
			return m, tea.Quit
		case "?":
			m.help = true
		case "tab":
			m.focus = (m.focus + 1) % 3
		case "shift+tab":
			m.focus = (m.focus + 2) % 3
		case "1":
			m.focus = 0
		case "2":
			m.focus = 1
		case "3":
			m.focus = 2
		case "r":
			return m, func() tea.Msg { runs, err := m.db.List(); return runsMsg{runs: runs, err: err} }
		case "s":
			m.slow = !m.slow
			m.rebuildRows("", false)
		case "a":
			m.subtree = !m.subtree
			m.refreshLogs()
		case "t":
			m.stream = (m.stream + 1) % 3
			m.refreshLogs()
		case "/":
			m.searching = true
			m.match = -1
			m.query = ""
			m.focus = 2
		case "esc":
			m.query = ""
		case "n":
			m.findMatch(1, false)
		case "N":
			m.findMatch(-1, false)
		case "up", "k":
			return m, m.move(-1)
		case "down", "j":
			return m, m.move(1)
		case "pgup", "ctrl+u":
			return m, m.move(-m.pageSize())
		case "pgdown", "ctrl+d":
			return m, m.move(m.pageSize())
		case "home", "g":
			return m, m.move(-1 << 30)
		case "end", "G":
			return m, m.move(1 << 30)
		case "left", "h":
			if m.focus == 2 {
				m.logX = max(0, m.logX-8)
			} else if m.focus == 1 {
				m.expand(false)
			}
		case "right", "l":
			if m.focus == 2 {
				m.logX += 8
			} else if m.focus == 1 {
				m.expand(true)
			}
		case "enter", "space":
			if m.focus == 0 {
				m.focus = 1
			} else if m.focus == 1 && len(m.rows) > 0 {
				m.expand(!m.expanded[m.rows[m.cursor].id])
			}
		case "E":
			if m.profile != nil {
				for id := range m.profile.Manifest.Targets {
					m.expanded[id] = true
				}
				m.rebuildRows("", false)
			}
		case "C":
			m.expanded = make(map[string]bool)
			m.rebuildRows("", false)
		}
	}
	return m, nil
}

func (m *Model) pageSize() int { return max(1, m.height-17) }

func (m *Model) move(delta int) tea.Cmd {
	switch m.focus {
	case 0:
		if len(m.runs) == 0 {
			return nil
		}
		next := max(0, min(len(m.runs)-1, m.runCursor+delta))
		if next != m.runCursor {
			m.runCursor = next
			return m.loadRun()
		}
	case 1:
		if len(m.rows) == 0 {
			return nil
		}
		m.cursor = max(0, min(len(m.rows)-1, m.cursor+delta))
		m.refreshLogs()
	case 2:
		m.logOffset += delta
		m.clampLogs()
	}
	return nil
}

func (m *Model) expand(expand bool) {
	if len(m.rows) == 0 {
		return
	}
	r := m.rows[m.cursor]
	if !r.target {
		m.focus = 2
		return
	}
	if r.shared {
		for i, candidate := range m.rows {
			if candidate.target && candidate.id == r.id && !candidate.shared {
				m.cursor = i
				m.refreshLogs()
				return
			}
		}
	}
	m.expanded[r.id] = expand
	m.rebuildRows(r.id, true)
}

func (m *Model) rebuildRows(id string, target bool) {
	if m.profile == nil {
		return
	}
	m.rows = treeRows(m.profile, m.expanded, m.slow)
	m.cursor = 0
	for i, r := range m.rows {
		if r.id == id && r.target == target {
			m.cursor = i
			break
		}
	}
	m.refreshLogs()
}

func (m *Model) refreshLogs() {
	m.logOffset, m.logX, m.match = 0, 0, -1
	m.logLines = nil
	if m.profile == nil || len(m.rows) == 0 {
		return
	}
	r := m.rows[m.cursor]
	var data []byte
	if r.target {
		data = m.profile.TargetLogData(r.id, m.subtree, m.stream)
	} else {
		data = m.profile.LogData(r.id, m.stream)
	}
	if len(data) > 0 {
		m.logLines = strings.Split(safeText(string(data)), "\n")
	}
}

func (m *Model) clampLogs() {
	m.logOffset = max(0, min(max(0, len(m.logLines)-m.pageSize()), m.logOffset))
}

func (m *Model) findMatch(direction int, includeCurrent bool) {
	if m.query == "" || len(m.logLines) == 0 {
		return
	}
	start := m.logOffset
	if m.match >= 0 && !includeCurrent {
		start = m.match
	}
	if includeCurrent {
		start -= direction
	}
	for step := 1; step <= len(m.logLines); step++ {
		i := ((start+step*direction)%len(m.logLines) + len(m.logLines)) % len(m.logLines)
		if strings.Contains(strings.ToLower(m.logLines[i]), strings.ToLower(m.query)) {
			m.logOffset = i
			m.match = i
			m.message = fmt.Sprintf("Match on line %d", i+1)
			m.clampLogs()
			return
		}
	}
	m.message = "No matching log lines"
}

func safeText(text string) string {
	text = ansi.Strip(strings.ToValidUTF8(text, "?"))
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r == '\n' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, text)
}

func oneLine(text string) string { return strings.Join(strings.Fields(safeText(text)), " ") }

func windowStart(cursor, length, height int) int {
	return max(0, min(max(0, length-height), cursor-height/2))
}

func sortedKeys[M ~map[string]V, V any](m M) []string {
	keys := make([]string, 0, len(m))
	for id := range m {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	return keys
}
