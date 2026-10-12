// Package store persists imported profiles and run summaries in a local bbolt database.
package store

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/EarthBuild/earthbuild-profiler/internal/profile"
	bolt "go.etcd.io/bbolt"
)

// Summary contains the small amount of metadata needed to browse run history.
type Summary struct {
	ID         string            `json:"id"`
	Target     string            `json:"target"`
	Status     string            `json:"status"`
	StartedAt  profile.Timestamp `json:"startedAtUnixNanos"`
	ImportedAt time.Time         `json:"importedAt"`
	Duration   *uint64           `json:"durationNanos"`
	Commands   int               `json:"commands"`
	Targets    int               `json:"targets"`
}

// Store opens the database per operation, so an idle viewer never blocks imports.
type Store struct{ path string }

// ErrNotFound identifies a missing run.
var ErrNotFound = errors.New("run not found")

// DefaultPath uses the XDG state directory or its conventional home fallback.
func DefaultPath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "earthbuild-profiler", "runs.db"), nil
}

// Open initializes a database and checks its schema. No lock is retained.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: path}
	err := s.withDB(false, func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			meta, err := tx.CreateBucketIfNotExists([]byte("meta"))
			if err != nil {
				return err
			}
			if version := meta.Get([]byte("version")); version != nil && string(version) != "1" {
				return fmt.Errorf("unsupported database version %q", version)
			}
			if err := meta.Put([]byte("version"), []byte("1")); err != nil {
				return err
			}
			for _, name := range []string{"profiles", "summaries"} {
				if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
					return err
				}
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) withDB(readOnly bool, fn func(*bolt.DB) error) error {
	db, err := bolt.Open(s.path, 0o600, &bolt.Options{Timeout: time.Second, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("open database %s: %w", s.path, err)
	}
	defer db.Close()
	return fn(db)
}

// Save atomically upserts a run keyed by build ID and returns its summary.
func (s *Store) Save(p *profile.Profile) (Summary, error) {
	summary := Summary{ID: p.Manifest.BuildID, Target: p.TargetName(), Status: p.Manifest.Status,
		StartedAt: p.Manifest.StartedAt, ImportedAt: time.Now().UTC(), Duration: p.RunDuration,
		Commands: len(p.Manifest.Commands), Targets: len(p.Manifest.Targets)}
	data, err := json.Marshal(p, json.Deterministic(true))
	if err != nil {
		return summary, err
	}
	err = s.withDB(false, func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			key := []byte(summary.ID)
			if old := tx.Bucket([]byte("summaries")).Get(key); old != nil {
				var previous Summary
				if err := json.Unmarshal(old, &previous); err != nil {
					return err
				}
				summary.ImportedAt = previous.ImportedAt
			}
			meta, err := json.Marshal(summary)
			if err != nil {
				return err
			}
			if err := tx.Bucket([]byte("profiles")).Put(key, data); err != nil {
				return err
			}
			return tx.Bucket([]byte("summaries")).Put(key, meta)
		})
	})
	return summary, err
}

// List returns run summaries with the most recently started runs first.
func (s *Store) List() ([]Summary, error) {
	var runs []Summary
	err := s.withDB(true, func(db *bolt.DB) error {
		return db.View(func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("summaries")).ForEach(func(_, value []byte) error {
				var r Summary
				if err := json.Unmarshal(value, &r); err != nil {
					return err
				}
				runs = append(runs, r)
				return nil
			})
		})
	})
	slices.SortFunc(runs, func(a, b Summary) int {
		if c := cmp.Compare(b.StartedAt, a.StartedAt); c != 0 {
			return c
		}
		if c := b.ImportedAt.Compare(a.ImportedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return runs, err
}

// Load retrieves an independent copy of one saved profile.
func (s *Store) Load(id string) (*profile.Profile, error) {
	var p profile.Profile
	err := s.withDB(true, func(db *bolt.DB) error {
		return db.View(func(tx *bolt.Tx) error {
			value := tx.Bucket([]byte("profiles")).Get([]byte(id))
			if value == nil {
				return fmt.Errorf("run %q: %w", id, ErrNotFound)
			}
			return json.Unmarshal(value, &p)
		})
	})
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Delete atomically removes one imported run and its summary.
func (s *Store) Delete(id string) error {
	return s.withDB(false, func(db *bolt.DB) error {
		return db.Update(func(tx *bolt.Tx) error {
			if tx.Bucket([]byte("profiles")).Get([]byte(id)) == nil {
				return fmt.Errorf("run %q: %w", id, ErrNotFound)
			}
			if err := tx.Bucket([]byte("profiles")).Delete([]byte(id)); err != nil {
				return err
			}
			return tx.Bucket([]byte("summaries")).Delete([]byte(id))
		})
	})
}
