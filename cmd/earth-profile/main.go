// Command earth-profile imports, stores, and browses EarthBuild run profiles.
package main

import (
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"text/tabwriter"

	tea "charm.land/bubbletea/v2"
	"github.com/EarthBuild/earthbuild-profiler/internal/profile"
	"github.com/EarthBuild/earthbuild-profiler/internal/store"
	"github.com/EarthBuild/earthbuild-profiler/internal/tui"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("earth-profile", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var dbPath, earth string
	flags.StringVar(&dbPath, "db", "", "Database path (default: XDG state directory)")
	flags.StringVar(&earth, "earth", "earth", "EarthBuild executable for the run command")
	flags.Usage = func() { fmt.Fprint(stderr, usage); flags.PrintDefaults() }
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	args = flags.Args()
	if len(args) == 0 {
		args = []string{"browse"}
	}
	if args[0] == "help" {
		flags.Usage()
		return 0
	}
	if !validCommand(args[0]) {
		fmt.Fprintf(stderr, "unknown command %q\n%s", args[0], usage)
		return 2
	}
	if dbPath == "" {
		var err error
		dbPath, err = store.DefaultPath()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	db, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	code, err := execute(db, earth, args, stdin, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "earth-profile:", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

func validCommand(command string) bool {
	switch command {
	case "import", "browse", "runs", "export", "logs", "delete", "run", "serve":
		return true
	default:
		return false
	}
}

func execute(db *store.Store, earth string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	command, args := args[0], args[1:]
	switch command {
	case "serve":
		return serve(db, args, stdout, stderr)
	case "import":
		if len(args) == 0 {
			args = []string{"-"}
		}
		stdinUsed := false
		for _, path := range args {
			if path == "-" && stdinUsed {
				return 2, errors.New("stdin can be imported only once")
			}
			p, err := readProfile(path, stdin)
			if err != nil {
				return 1, fmt.Errorf("import %s: %w", path, err)
			}
			if path == "-" {
				stdinUsed = true
			}
			s, err := db.Save(p)
			if err != nil {
				return 1, err
			}
			fmt.Fprintf(stdout, "Imported %s  %s  %s  %s\n", s.ID, s.Target, profile.Duration(s.Duration), profile.Status(s.Status))
		}
	case "runs":
		if len(args) != 0 {
			return 2, errors.New("usage: earth-profile runs")
		}
		runs, err := db.List()
		if err != nil {
			return 1, err
		}
		w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "BUILD ID\tTARGET\tDURATION\tSTATUS\tSTEPS")
		for _, r := range runs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n", r.ID, r.Target, profile.Duration(r.Duration), profile.Status(r.Status), r.Commands)
		}
		return 0, w.Flush()
	case "export":
		if len(args) < 1 || len(args) > 2 {
			return 2, errors.New("usage: earth-profile export BUILD_ID [FILE|-]")
		}
		p, err := db.Load(args[0])
		if err != nil {
			return 1, err
		}
		if len(args) == 1 || args[1] == "-" {
			return 0, json.MarshalWrite(stdout, p, json.Deterministic(true))
		}
		return 0, writeProfile(args[1], p)
	case "logs":
		f := flag.NewFlagSet("logs", flag.ContinueOnError)
		f.SetOutput(stderr)
		stream := f.Int("stream", 0, "0: stdout + stderr, 1: stdout, 2: stderr, other: raw stream number")
		if err := f.Parse(args); err != nil {
			return 2, err
		}
		args = f.Args()
		if len(args) != 2 || *stream < 0 || int64(*stream) > 1<<31-1 {
			return 2, errors.New("usage: earth-profile logs [--stream N] BUILD_ID COMMAND_ID")
		}
		p, err := db.Load(args[0])
		if err != nil {
			return 1, err
		}
		if _, ok := p.Manifest.Commands[args[1]]; !ok {
			if _, ok := p.Logs[args[1]]; !ok {
				return 1, fmt.Errorf("command %q not found", args[1])
			}
		}
		_, err = stdout.Write(p.LogData(args[1], int32(*stream)))
		return 0, err
	case "delete":
		if len(args) != 1 {
			return 2, errors.New("usage: earth-profile delete BUILD_ID")
		}
		return 0, db.Delete(args[0])
	case "run":
		if len(args) == 0 {
			return 2, errors.New("usage: earth-profile run [earth build arguments]")
		}
		if args[0] == "--" {
			args = args[1:]
		}
		if len(args) == 0 {
			return 2, errors.New("missing earth build arguments")
		}
		return capture(db, earth, args, stdin, stdout, stderr)
	case "browse":
		if len(args) > 1 {
			return 2, errors.New("usage: earth-profile browse [BUILD_ID]")
		}
		runs, err := db.List()
		if err != nil {
			return 1, err
		}
		id := ""
		if len(args) == 1 {
			id = args[0]
			if _, err := db.Load(id); err != nil {
				return 1, err
			}
		}
		_, err = tea.NewProgram(tui.New(db, runs, id), tea.WithInput(stdin), tea.WithOutput(stdout)).Run()
		return 0, err
	}
	return 0, nil
}

func readProfile(path string, stdin io.Reader) (*profile.Profile, error) {
	if path == "-" {
		return profile.Decode(stdin)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return profile.Decode(f)
}

func writeProfile(path string, p *profile.Profile) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".earth-profile-export-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.MarshalWrite(f, p, json.Deterministic(true))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err := errors.Join(err, closeErr); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func capture(db *store.Store, earth string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	dir, err := os.MkdirTemp("", "earth-profile-run-*")
	if err != nil {
		return 1, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "profile.json")
	cmd := exec.Command(earth, append([]string{"--profile", path}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	// A terminal interrupt is delivered to both parent and child. Keep the
	// wrapper alive long enough to import the child's canceled-run profile.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("start earth: %w", err)
	}
	waitErr := cmd.Wait()
	code := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code = exitErr.ExitCode()
			if code < 0 {
				code = 130
			}
		} else {
			code = 1
		}
	}
	p, err := readProfile(path, stdin)
	if err != nil {
		return max(1, code), fmt.Errorf("earth finished without a usable profile; use an earth build supporting --profile: %w", err)
	}
	s, err := db.Save(p)
	if err != nil {
		return max(1, code), err
	}
	fmt.Fprintf(stderr, "\nSaved profile %s (%s, %s). Browse with: earth-profile browse %s\n", s.ID, profile.Duration(s.Duration), profile.Status(s.Status), s.ID)
	return code, nil
}

const usage = `EarthBuild Profiler

Usage: earth-profile [--db PATH] [--earth EXECUTABLE] COMMAND

  import [FILE ...]            Import profiles; '-' or no file reads stdin
  run [EARTH ARGUMENTS ...]    Capture a build and import its profile
  browse [BUILD_ID]            Open the TUI (default command)
  serve [--listen HOST:PORT]   Serve the web viewer and upload API
  runs                        List saved run summaries
  logs [--stream N] ID STEP    Write a step's original log bytes
  export BUILD_ID [FILE|-]     Export a saved profile
  delete BUILD_ID              Remove a saved run
  help                        Show usage

Global flags go before the command. Import stdin separately, then run browse
from an interactive terminal. Profiles are imported after a build finishes.

`
