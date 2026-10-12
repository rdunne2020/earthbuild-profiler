package tui

import (
	"cmp"
	"slices"

	"github.com/EarthBuild/earthbuild-profiler/internal/profile"
)

type row struct {
	id       string
	target   bool
	shared   bool
	depth    int
	label    string
	duration *uint64
	status   string
	cached   bool
}

func treeRows(p *profile.Profile, expanded map[string]bool, slow bool) []row {
	var rows []row
	if slow {
		for id, c := range p.Manifest.Commands {
			name := p.Manifest.Targets[c.TargetID].Name
			if name == "" {
				name = c.Category
			}
			rows = append(rows, row{id: id, label: name + " | " + c.Name,
				duration: p.CommandDurations[id], status: c.Status, cached: c.Cached})
		}
		slices.SortFunc(rows, func(a, b row) int {
			if a.duration == nil && b.duration != nil {
				return 1
			}
			if a.duration != nil && b.duration == nil {
				return -1
			}
			if a.duration != nil && b.duration != nil {
				if c := cmp.Compare(*b.duration, *a.duration); c != 0 {
					return c
				}
			}
			return cmp.Compare(a.id, b.id)
		})
		return rows
	}
	seen := make(map[string]bool)
	var visit func(string, int)
	visit = func(id string, depth int) {
		t := p.Manifest.Targets[id]
		name := t.Name
		if name == "" {
			name = id
		}
		rows = append(rows, row{id: id, target: true, shared: seen[id], depth: depth,
			label: name, duration: p.TargetDurations[id], status: t.Status})
		if seen[id] {
			return
		}
		seen[id] = true
		if !expanded[id] {
			return
		}
		for _, cid := range p.CommandIDs(id) {
			c := p.Manifest.Commands[cid]
			rows = append(rows, row{id: cid, depth: depth + 1, label: c.Name,
				duration: p.CommandDurations[cid], status: c.Status, cached: c.Cached})
		}
		deps := slices.Clone(t.DependsOn)
		slices.SortFunc(deps, func(a, b string) int {
			if c := cmp.Compare(p.Manifest.Targets[a].Name, p.Manifest.Targets[b].Name); c != 0 {
				return c
			}
			return cmp.Compare(a, b)
		})
		for _, dep := range deps {
			visit(dep, depth+1)
		}
	}
	if p.Manifest.MainTargetID != "" {
		visit(p.Manifest.MainTargetID, 0)
	}
	// Include disconnected targets (including cycles) without traversing nodes
	// hidden by a collapsed parent a second time at the top level.
	reachable := p.Descendants(p.Manifest.MainTargetID)
	ids := make([]string, 0, len(p.Manifest.Targets))
	for id := range p.Manifest.Targets {
		if !reachable[id] {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		if !seen[id] {
			visit(id, 0)
		}
	}
	for _, id := range p.CommandIDs("") {
		c := p.Manifest.Commands[id]
		rows = append(rows, row{id: id, label: c.Category + " | " + c.Name,
			duration: p.CommandDurations[id], status: c.Status, cached: c.Cached})
	}
	// Generic CLI messages have log IDs without a corresponding command.
	var generic []string
	for id := range p.Logs {
		if _, ok := p.Manifest.Commands[id]; !ok {
			generic = append(generic, id)
		}
	}
	slices.Sort(generic)
	for _, id := range generic {
		rows = append(rows, row{id: id, label: "CLI logs: " + id})
	}
	return rows
}
