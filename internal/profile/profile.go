// Package profile reads the versioned EarthBuild profile export without depending
// on the build tool or its protobuf implementation.
package profile

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Timestamp is an unsigned nanosecond timestamp. Protobuf JSON encodes it as a
// decimal string, while native Go JSON can encode it as a number.
type Timestamp uint64

// UnmarshalJSON accepts both lossless integer encodings.
func (t *Timestamp) UnmarshalJSON(data []byte) error {
	var value string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
	} else {
		value = string(data)
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid nanosecond timestamp: %w", err)
	}
	*t = Timestamp(n)
	return nil
}

// MarshalJSON preserves the protobuf JSON decimal-string representation.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	return strconv.AppendQuote(nil, strconv.FormatUint(uint64(t), 10)), nil
}

// SourceLocation identifies a command's Earthfile definition.
type SourceLocation struct {
	Extra          map[string]jsontext.Value `json:",embed"`
	File           string                    `json:"file"`
	StartLine      int                       `json:"startLine"`
	RepositoryURL  string                    `json:"repositoryUrl"`
	RepositoryHash string                    `json:"repositoryHash"`
}

// Reference is a dependency from a command to a target instance.
type Reference struct {
	Extra          map[string]jsontext.Value `json:",embed"`
	TargetID       string                    `json:"targetId"`
	ReferencedName string                    `json:"referencedName"`
}

// Target is one target instance, including its graph links and lifecycle times.
type Target struct {
	Extra           map[string]jsontext.Value `json:",embed"`
	Name            string                    `json:"name"`
	CanonicalName   string                    `json:"canonicalName"`
	OverrideArgs    []string                  `json:"overrideArgs"`
	InitialPlatform string                    `json:"initialPlatform"`
	FinalPlatform   string                    `json:"finalPlatform"`
	Status          string                    `json:"status,omitzero"`
	StartedAt       Timestamp                 `json:"startedAtUnixNanos"`
	EndedAt         Timestamp                 `json:"endedAtUnixNanos"`
	DependsOn       []string                  `json:"dependsOn"`
}

// Command is a step belonging to a target or an auxiliary build category.
type Command struct {
	Extra        map[string]jsontext.Value `json:",embed"`
	Name         string                    `json:"name"`
	TargetID     string                    `json:"targetId"`
	Category     string                    `json:"category"`
	Platform     string                    `json:"platform"`
	Status       string                    `json:"status,omitzero"`
	Cached       bool                      `json:"isCached"`
	Local        bool                      `json:"isLocal"`
	Interactive  bool                      `json:"isInteractive"`
	StartedAt    Timestamp                 `json:"startedAtUnixNanos"`
	EndedAt      Timestamp                 `json:"endedAtUnixNanos"`
	ErrorMessage string                    `json:"errorMessage"`
	Source       *SourceLocation           `json:"sourceLocation"`
	DependsOn    []Reference               `json:"dependsOn"`
}

// Failure describes the terminal build error.
type Failure struct {
	Extra        map[string]jsontext.Value `json:",embed"`
	Type         string                    `json:"type,omitzero"`
	TargetID     string                    `json:"targetId"`
	CommandID    string                    `json:"commandId"`
	ErrorMessage string                    `json:"errorMessage"`
	HelpMessage  string                    `json:"helpMessage"`
	Output       []byte                    `json:"output"`
}

// Manifest describes a complete recorded run.
type Manifest struct {
	Extra        map[string]jsontext.Value `json:",embed"`
	BuildID      string                    `json:"buildId"`
	Version      int                       `json:"version"`
	CreatedAt    Timestamp                 `json:"createdAtUnixNanos"`
	StartedAt    Timestamp                 `json:"startedAtUnixNanos"`
	EndedAt      Timestamp                 `json:"endedAtUnixNanos"`
	Status       string                    `json:"status,omitzero"`
	MainTargetID string                    `json:"mainTargetId"`
	Targets      map[string]Target         `json:"targets"`
	Commands     map[string]Command        `json:"commands"`
	Failure      *Failure                  `json:"failure"`
}

// Chunk preserves the raw bytes, source stream, and timestamp of command output.
type Chunk struct {
	Extra     map[string]jsontext.Value `json:",embed"`
	TargetID  string                    `json:"targetId"`
	Stream    int32                     `json:"stream"`
	Timestamp uint64                    `json:"timestampUnixNanos"`
	Data      []byte                    `json:"data"`
}

// Profile is the version 1 export. Logs and durations are indexed by instance ID.
type Profile struct {
	Extra            map[string]jsontext.Value `json:",embed"`
	Version          int                       `json:"version"`
	Manifest         Manifest                  `json:"manifest"`
	RunDuration      *uint64                   `json:"runDurationNanos"`
	TargetDurations  map[string]*uint64        `json:"targetDurationNanos"`
	CommandDurations map[string]*uint64        `json:"commandDurationNanos"`
	Logs             map[string][]Chunk        `json:"logs"`
}

// Decode validates one JSON profile, rejecting trailing data and oversized input.
func Decode(r io.Reader) (*Profile, error) {
	const maxSize = 512 << 20
	data, err := io.ReadAll(io.LimitReader(r, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read profile: %w", err)
	}
	if len(data) > maxSize {
		return nil, errors.New("profile exceeds 512 MiB import limit")
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("decode profile: %w", err)
	}
	if p.Version != 1 {
		return nil, fmt.Errorf("unsupported profile version %d (want 1)", p.Version)
	}
	if p.Manifest.Version != 0 && p.Manifest.Version != 2 {
		return nil, fmt.Errorf("unsupported manifest version %d", p.Manifest.Version)
	}
	if strings.TrimSpace(p.Manifest.BuildID) == "" {
		return nil, errors.New("profile has no build ID")
	}
	if id := p.Manifest.MainTargetID; id != "" {
		if _, ok := p.Manifest.Targets[id]; !ok {
			return nil, fmt.Errorf("main target %q is missing", id)
		}
	}
	for id, target := range p.Manifest.Targets {
		for _, dep := range target.DependsOn {
			if _, ok := p.Manifest.Targets[dep]; !ok {
				return nil, fmt.Errorf("target %q references missing target %q", id, dep)
			}
		}
	}
	for id, command := range p.Manifest.Commands {
		if command.TargetID != "" {
			if _, ok := p.Manifest.Targets[command.TargetID]; !ok {
				return nil, fmt.Errorf("command %q references missing target %q", id, command.TargetID)
			}
		}
		for _, dep := range command.DependsOn {
			if _, ok := p.Manifest.Targets[dep.TargetID]; !ok {
				return nil, fmt.Errorf("command %q references missing dependency %q", id, dep.TargetID)
			}
		}
	}
	return &p, nil
}

// TargetName returns the root target's display name, or a useful fallback.
func (p *Profile) TargetName() string {
	if t, ok := p.Manifest.Targets[p.Manifest.MainTargetID]; ok && t.Name != "" {
		return t.Name
	}
	return "(no target)"
}

// CommandIDs returns a target's steps in deterministic lifecycle/source order.
func (p *Profile) CommandIDs(targetID string) []string {
	var ids []string
	for id, c := range p.Manifest.Commands {
		if c.TargetID == targetID {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(a, b string) int {
		x, y := p.Manifest.Commands[a], p.Manifest.Commands[b]
		if c := cmp.Compare(x.StartedAt, y.StartedAt); c != 0 {
			return c
		}
		if x.Source != nil && y.Source != nil {
			if c := cmp.Compare(x.Source.StartLine, y.Source.StartLine); c != 0 {
				return c
			}
		}
		if c := cmp.Compare(x.Name, y.Name); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	return ids
}

// Descendants returns the selected target and its dependencies, counting shared
// targets once and tolerating cycles defensively.
func (p *Profile) Descendants(id string) map[string]bool {
	seen := make(map[string]bool)
	pending := []string{id}
	for len(pending) > 0 {
		id = pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		pending = append(pending, p.Manifest.Targets[id].DependsOn...)
	}
	return seen
}

// LogData returns only the requested command's output. Stream 0 merges stdout
// and stderr; explicit streams also allow inspection of execution statistics.
func (p *Profile) LogData(commandID string, stream int32) []byte {
	var data []byte
	for _, c := range p.Logs[commandID] {
		if matchesStream(c.Stream, stream) {
			data = append(data, c.Data...)
		}
	}
	return data
}

// TargetLogData merges a target's command output by recorded timestamp. Including
// dependencies counts each shared target once.
func (p *Profile) TargetLogData(targetID string, dependencies bool, stream int32) []byte {
	targets := map[string]bool{targetID: true}
	if dependencies {
		targets = p.Descendants(targetID)
	}
	ids := make([]string, 0, len(p.Logs))
	for id := range p.Logs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var chunks []Chunk
	for _, id := range ids {
		for _, c := range p.Logs[id] {
			if targets[c.TargetID] && matchesStream(c.Stream, stream) {
				chunks = append(chunks, c)
			}
		}
	}
	slices.SortStableFunc(chunks, func(a, b Chunk) int { return cmp.Compare(a.Timestamp, b.Timestamp) })
	var data []byte
	for _, c := range chunks {
		data = append(data, c.Data...)
	}
	return data
}

func matchesStream(actual, selected int32) bool {
	return actual == selected || (selected == 0 && (actual == 1 || actual == 2))
}

// Duration formats a nullable nanosecond duration without overflowing time.Duration.
func Duration(n *uint64) string {
	if n == nil {
		return "unknown"
	}
	if *n < uint64(time.Millisecond) {
		return time.Duration(*n).String()
	}
	if *n <= uint64(1<<63-1) {
		return time.Duration(*n).Round(time.Millisecond).String()
	}
	return fmt.Sprintf("%.1fs", float64(*n)/1e9)
}

// Status shortens the protobuf status enum for display.
func Status(status string) string {
	if status == "" {
		return "UNKNOWN"
	}
	return strings.TrimPrefix(status, "RUN_STATUS_")
}
