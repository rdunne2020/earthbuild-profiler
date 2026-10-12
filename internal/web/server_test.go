package web

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/EarthBuild/earthbuild-profiler/internal/profile"
	"github.com/EarthBuild/earthbuild-profiler/internal/store"
)

func TestUploadAndBrowse(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	h := New(db, "secret", "")
	request := func(method, path string, body []byte, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/runs", nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request("GET", "/", nil, ""); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("Build profiler")) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("GET", "/api/runs", nil, "secret"); w.Code != 200 || w.Body.String() != "[]" {
		t.Fatal(w.Code, w.Body.String())
	}
	data, err := os.ReadFile("../../testdata/demo.json")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if w := request("POST", "/api/runs", data, "secret"); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	runs, err := db.List()
	if err != nil || len(runs) != 1 {
		t.Fatal(runs, err)
	}
	p, err := profile.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/runs/" + p.Manifest.BuildID
	w := request("GET", path, nil, "secret")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var view profile.Profile
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || len(view.Logs) != 0 || len(view.Manifest.Commands) == 0 {
		t.Fatal(err, view)
	}
	w = request("GET", path+"/export", nil, "secret")
	exported, err := profile.Decode(w.Body)
	if err != nil || len(exported.Logs) != len(p.Logs) {
		t.Fatal(err)
	}
	for id := range p.Manifest.Commands {
		w = request("GET", path+"/logs?command="+id+"&stream=1", nil, "secret")
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), p.LogData(id, 1)) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w = request("GET", path+"/logs?target=all&dependencies=true", nil, "secret")
	if !bytes.Equal(w.Body.Bytes(), p.TargetLogData("all", true, 0)) {
		t.Fatal("subtree logs differ")
	}
	for _, test := range []struct {
		method, path string
		body         []byte
		code         int
	}{
		{"POST", "/api/runs", []byte(`{"version":99}`), 400},
		{"GET", "/api/runs/missing", nil, 404},
		{"GET", path + "/logs?stream=-1", nil, 400},
		{"GET", path + "/logs?command=missing", nil, 404},
		{"GET", path + "/logs", nil, 400},
		{"DELETE", path, nil, 405},
	} {
		w := request(test.method, test.path, test.body, "secret")
		if w.Code != test.code {
			t.Errorf("%s: got %d want %d", test.path, w.Code, test.code)
		}
	}
}

func TestConcurrentImports(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(db, "", ""))
	defer server.Close()
	data, err := os.ReadFile("../../testdata/demo.json")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			response, err := http.Post(server.URL+"/api/runs", "application/json", bytes.NewReader(data))
			if err != nil {
				t.Error(err)
				return
			}
			defer response.Body.Close()
			body, _ := io.ReadAll(response.Body)
			if response.StatusCode != 200 {
				t.Errorf("upload: %d %s", response.StatusCode, body)
			}
		})
	}
	wg.Wait()
	runs, err := db.List()
	if err != nil || len(runs) != 1 {
		t.Fatal(runs, err)
	}
}

func TestUploadResultLink(t *testing.T) {
	for _, tc := range []struct{ name, requestURL, publicURL, origin string }{
		{"request host", "http://profiler.example:8080/api/runs", "", "http://profiler.example:8080"},
		{"TLS", "https://profiler.example/api/runs", "", "https://profiler.example"},
		{"public origin", "http://internal:8080/api/runs", "https://builds.example.com/", "https://builds.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "runs.db"))
			if err != nil {
				t.Fatal(err)
			}
			p := &profile.Profile{Version: 1, Manifest: profile.Manifest{Version: 2, BuildID: "older / build?&+#日本", StartedAt: 1}}
			newer := &profile.Profile{Version: 1, Manifest: profile.Manifest{Version: 2, BuildID: "newest-started", StartedAt: 2}}
			if _, err := db.Save(newer); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			h := New(db, "", tc.publicURL)
			for range 2 {
				r := httptest.NewRequest("POST", tc.requestURL, bytes.NewReader(data))
				r.Header.Set("X-Forwarded-Host", "untrusted.example")
				r.Header.Set("X-Forwarded-Proto", "https")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				var result struct {
					ID  string `json:"id"`
					URL string `json:"url"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 {
					t.Fatal(err, w.Code, w.Body.String())
				}
				link, err := url.Parse(result.URL)
				if err != nil {
					t.Fatal(err)
				}
				if link.Scheme+"://"+link.Host != tc.origin || link.Path != "/" || link.Query().Get("run") != p.Manifest.BuildID || result.ID != p.Manifest.BuildID || w.Header().Get("Location") != result.URL {
					t.Fatal(result, w.Header())
				}
				view := httptest.NewRecorder()
				h.ServeHTTP(view, httptest.NewRequest("GET", result.URL, nil))
				if view.Code != 200 {
					t.Fatal(view.Code)
				}
			}
		})
	}
}

func TestDocumentation(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	h := New(db, "secret", "")
	for _, path := range []string{"/docs/", "/docs/init.js", "/docs/vendor/swagger-ui-bundle.js", "/docs/vendor/swagger-ui.css", "/openapi.json"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		if path == "/docs/" && !bytes.Contains(w.Body.Bytes(), []byte("Swagger UI")) {
			t.Fatal("missing Swagger page")
		}
		if path == "/openapi.json" {
			var spec struct {
				OpenAPI string                    `json:"openapi"`
				Paths   map[string]map[string]any `json:"paths"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
				t.Fatal(err)
			}
			if spec.OpenAPI != "3.0.3" {
				t.Fatal(spec.OpenAPI)
			}
			for _, operation := range []struct{ path, method string }{{"/healthz", "get"}, {"/api/runs", "get"}, {"/api/runs", "post"}, {"/api/runs/{id}", "get"}, {"/api/runs/{id}/export", "get"}, {"/api/runs/{id}/logs", "get"}} {
				if _, ok := spec.Paths[operation.path][operation.method]; !ok {
					t.Fatalf("undocumented: %s %s", operation.method, operation.path)
				}
			}
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/docs", nil))
	if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "/docs/" {
		t.Fatal(w.Code, w.Header())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/runs", nil))
	if w.Code != 401 {
		t.Fatal("docs changed API authentication")
	}
}
