// Package web serves the embedded profile viewer and upload API.
package web

import (
	"crypto/subtle"
	"embed"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/EarthBuild/earthbuild-profiler/internal/profile"
	"github.com/EarthBuild/earthbuild-profiler/internal/store"
)

//go:embed static/*
var assets embed.FS

//go:embed docs/*
var documentation embed.FS

// New serves profiles from db. An optional token protects every API route.
// publicURL overrides the request origin when generating browser links.
func New(db *store.Store, token, publicURL string) http.Handler {
	mux := http.NewServeMux()
	docs, _ := fs.Sub(documentation, "docs")
	mux.Handle("GET /docs/", http.StripPrefix("/docs/", http.FileServer(http.FS(docs))))
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		data, _ := documentation.ReadFile("docs/openapi.json")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, r *http.Request) {
		runs, err := db.List()
		if err != nil {
			fail(w, 500, "could not list runs")
			return
		}
		if runs == nil {
			runs = []store.Summary{}
		}
		respond(w, 200, runs)
	})
	mux.HandleFunc("POST /api/runs", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 512<<20)
		p, err := profile.Decode(r.Body)
		if err != nil {
			var large *http.MaxBytesError
			code := 400
			if errors.As(err, &large) {
				code = 413
			}
			fail(w, code, err.Error())
			return
		}
		summary, err := db.Save(p)
		if err != nil {
			fail(w, 500, "could not save profile")
			return
		}
		origin := publicURL
		if origin == "" {
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			origin = scheme + "://" + r.Host
		}
		viewURL := strings.TrimRight(origin, "/") + "/?" + url.Values{"run": {summary.ID}}.Encode()
		w.Header().Set("Location", viewURL)
		respond(w, 200, struct {
			store.Summary `json:",embed"`
			URL           string `json:"url"`
		}{summary, viewURL})
		_, _ = w.Write([]byte("\n"))
	})
	load := func(w http.ResponseWriter, r *http.Request) *profile.Profile {
		p, err := db.Load(r.PathValue("id"))
		if err != nil {
			code := 500
			message := "could not load profile"
			if errors.Is(err, store.ErrNotFound) {
				code = 404
				message = "run not found"
			}
			fail(w, code, message)
			return nil
		}
		return p
	}
	mux.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		p := load(w, r)
		if p == nil {
			return
		}
		p.Logs = nil
		respond(w, 200, p)
	})
	mux.HandleFunc("GET /api/runs/{id}/export", func(w http.ResponseWriter, r *http.Request) {
		p := load(w, r)
		if p == nil {
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="profile.json"`)
		respond(w, 200, p)
	})
	mux.HandleFunc("GET /api/runs/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		stream, err := strconv.ParseInt(defaultValue(r.URL.Query().Get("stream"), "0"), 10, 32)
		if err != nil || stream < 0 {
			fail(w, 400, "invalid stream")
			return
		}
		p := load(w, r)
		if p == nil {
			return
		}
		var data []byte
		if id := r.URL.Query().Get("command"); id != "" {
			if _, ok := p.Manifest.Commands[id]; !ok {
				fail(w, 404, "command not found")
				return
			}
			data = p.LogData(id, int32(stream))
		} else if id := r.URL.Query().Get("target"); id != "" {
			if _, ok := p.Manifest.Targets[id]; !ok {
				fail(w, 404, "target not found")
				return
			}
			data = p.TargetLogData(id, r.URL.Query().Get("dependencies") == "true", int32(stream))
		} else {
			fail(w, 400, "select a command or target")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(data)
	})
	files, _ := fs.Sub(assets, "static")
	mux.Handle("GET /", http.FileServer(http.FS(files)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/docs/") {
			// Swagger UI uses inline styles for its interactive components.
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
				fail(w, 401, "API token required")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func defaultValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.MarshalWrite(w, value)
}
func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
