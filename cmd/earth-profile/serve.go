package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/EarthBuild/earthbuild-profiler/internal/store"
	"github.com/EarthBuild/earthbuild-profiler/internal/web"
)

func serve(db *store.Store, args []string, stdout, stderr io.Writer) (int, error) {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("listen", "127.0.0.1:8080", "HTTP listen address")
	publicURL := flags.String("public-url", os.Getenv("EARTH_PROFILE_PUBLIC_URL"), "Public HTTP(S) origin for uploaded run links (or EARTH_PROFILE_PUBLIC_URL)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, err
	}
	if len(flags.Args()) != 0 {
		return 2, errors.New("usage: earth-profile serve [--listen HOST:PORT] [--public-url URL]")
	}
	if *publicURL != "" {
		u, err := url.Parse(*publicURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return 2, errors.New("public-url must be an absolute HTTP(S) origin, such as https://profiler.example.com")
		}
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return 1, err
	}
	server := &http.Server{Handler: web.New(db, os.Getenv("EARTH_PROFILE_TOKEN"), *publicURL), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if server.Shutdown(shutdown) != nil {
				_ = server.Close()
			}
		case <-done:
		}
	}()
	fmt.Fprintf(stdout, "EarthBuild Profiler listening on http://%s\n", listener.Addr())
	err = server.Serve(listener)
	close(done)
	if errors.Is(err, http.ErrServerClosed) {
		return 0, nil
	}
	return 1, err
}
