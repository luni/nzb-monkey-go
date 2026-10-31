package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// sabnzbd.ini fixture with two nested server sections
const testSabnzbdIni = `
[misc]
api_key = TESTAPIKEY123
host = 10.0.0.1
port = 8080
enable_https = 0

[servers]
[[reader.a.example]]
host = news.a.example
port = 563
ssl = 1
username = user_a
password = pass_a
connections = 50
enable = 1
priority = 5

[[reader.b.example]]
host = news.b.example
port = 119
ssl = 0
username = user_b
password = pass_b
connections = 10
enable = 1
priority = 0

[[reader.c.example]]
host = news.c.example
port = 443
ssl = 1
username = user_c
password = pass_c
connections = 30
enable = 0
priority = 1

[categories]
[[*]]
dir = ""
priority = 0
`

func writeSabnzbdIni(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sabnzbd.ini")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write sabnzbd.ini fixture: %v", err)
	}
	return path
}

func TestSelectSabnzbdServer(t *testing.T) {
	c := Configuration{Sabnzbd: SABnzbd{UseServerCreds: true}}
	if err := importSabnzbdIni(&c, writeSabnzbdIni(t, testSabnzbdIni)); err != nil {
		t.Fatalf("importSabnzbdIni failed: %v", err)
	}
	// priority 0 beats priority 5; disabled server is ignored
	if c.Directsearch.Host != "news.b.example" {
		t.Fatalf("expected lowest-priority enabled server, got %q", c.Directsearch.Host)
	}
	if c.Directsearch.Port != 119 || c.Directsearch.SSL {
		t.Fatalf("unexpected port/ssl import: %d/%v", c.Directsearch.Port, c.Directsearch.SSL)
	}
	if c.Directsearch.Username != "user_b" || c.Directsearch.Password != "pass_b" {
		t.Fatalf("unexpected credential import")
	}
	if c.Directsearch.Connections != 10 {
		t.Fatalf("unexpected connections import: %d", c.Directsearch.Connections)
	}
	if c.Sabnzbd.Nzbkey != "TESTAPIKEY123" || c.Sabnzbd.Host != "10.0.0.1" || c.Sabnzbd.Port != 8080 {
		t.Fatalf("unexpected SABNZBD target import: %q %q %d", c.Sabnzbd.Nzbkey, c.Sabnzbd.Host, c.Sabnzbd.Port)
	}
}

func TestImportSabnzbdIniKeepsExplicitValues(t *testing.T) {
	c := Configuration{
		Sabnzbd: SABnzbd{UseServerCreds: true, Nzbkey: "OWNKEY", Host: "sab.local", Port: 9090},
		Directsearch: DirectSearch{
			Host:     "news.own.example",
			Username: "explicit_user",
			Password: "explicit_pass",
		},
	}
	if err := importSabnzbdIni(&c, writeSabnzbdIni(t, testSabnzbdIni)); err != nil {
		t.Fatalf("importSabnzbdIni failed: %v", err)
	}
	// explicit directsearch host blocks the server import entirely
	if c.Directsearch.Host != "news.own.example" {
		t.Fatalf("explicit host overwritten: %q", c.Directsearch.Host)
	}
	if c.Directsearch.Username != "explicit_user" || c.Directsearch.Password != "explicit_pass" {
		t.Fatal("explicit credentials overwritten")
	}
	// explicit SABNZBD values must survive
	if c.Sabnzbd.Nzbkey != "OWNKEY" || c.Sabnzbd.Host != "sab.local" || c.Sabnzbd.Port != 9090 {
		t.Fatal("explicit SABNZBD values overwritten")
	}
}

func TestImportSabnzbdIniDisabled(t *testing.T) {
	c := Configuration{Sabnzbd: SABnzbd{UseServerCreds: false}}
	if err := importSabnzbdIni(&c, writeSabnzbdIni(t, testSabnzbdIni)); err != nil {
		t.Fatalf("importSabnzbdIni failed: %v", err)
	}
	if c.Directsearch.Host != "" {
		t.Fatal("server credentials imported despite use_server_credentials = false")
	}
	// api_key import is independent of the server creds toggle
	if c.Sabnzbd.Nzbkey != "TESTAPIKEY123" {
		t.Fatal("api_key not imported")
	}
}

func TestImportSabnzbdIniMissingFile(t *testing.T) {
	c := Configuration{Sabnzbd: SABnzbd{UseServerCreds: true}}
	err := importSabnzbdIni(&c, filepath.Join(t.TempDir(), "nonexistent.ini"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if strings.Contains(err.Error(), "pass_a") || strings.Contains(err.Error(), "TESTAPIKEY") {
		t.Fatal("error message leaks secrets")
	}
}

func TestSabnzbdAuthMethods(t *testing.T) {
	for _, tc := range []struct {
		response string
		want     map[string]bool
	}{
		{`{"auth":"apikey"}`, map[string]bool{"apikey": true}},
		{`{"auth":"login"}`, map[string]bool{"login": true}},
		{`{"auth":{"login":true,"apikey":true}}`, map[string]bool{"login": true, "apikey": true}},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, tc.response)
		}))
		methods, err := sabnzbdAuthMethods(srv.URL)
		srv.Close()
		if err != nil {
			t.Fatalf("sabnzbdAuthMethods failed: %v", err)
		}
		if len(methods) != len(tc.want) {
			t.Fatalf("%s: expected %v, got %v", tc.response, tc.want, methods)
		}
		for k := range tc.want {
			if !methods[k] {
				t.Fatalf("%s: missing method %q in %v", tc.response, k, methods)
			}
		}
	}
}

func TestSabnzbdLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login/" || r.Method != http.MethodPost {
			t.Errorf("unexpected login request %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("failed to parse login form: %v", err)
		}
		if r.PostForm.Get("username") == "sab" && r.PostForm.Get("password") == "secret" {
			http.SetCookie(w, &http.Cookie{Name: "login_cookie", Value: "sessionid"})
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	ok, err := sabnzbdLogin(srv.URL, "sab", "secret")
	if err != nil || !ok {
		t.Fatalf("expected successful login, got ok=%v err=%v", ok, err)
	}
	ok, err = sabnzbdLogin(srv.URL, "sab", "wrong")
	if err != nil || ok {
		t.Fatalf("expected failed login, got ok=%v err=%v", ok, err)
	}
}

func TestSabPauseCoordinator(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	paused := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Query().Get("mode") {
		case "pause":
			calls = append(calls, "pause")
			paused = true
			fmt.Fprint(w, `{"status":true}`)
		case "resume":
			calls = append(calls, "resume")
			paused = false
			fmt.Fprint(w, `{"status":true}`)
		case "queue":
			fmt.Fprintf(w, `{"queue":{"paused":%v}}`, paused)
		default:
			t.Errorf("unexpected api mode %q", r.URL.Query().Get("mode"))
		}
	}))
	defer srv.Close()

	c := &sabPauseCoordinator{baseURL: srv.URL, apikey: "key", settle: time.Millisecond, pollWait: 2 * time.Second}
	release, err := c.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if !paused {
		t.Fatal("queue not paused after acquire")
	}
	release()
	mu.Lock()
	defer mu.Unlock()
	if paused {
		t.Fatal("queue still paused after release")
	}
	if len(calls) != 2 || calls[0] != "pause" || calls[1] != "resume" {
		t.Fatalf("unexpected call sequence: %v", calls)
	}
}

// testServeState returns a state wired to a stubbed subprocess runner and
// restores mutated globals when the test ends.
func testServeState(t *testing.T, execFn func(ctx context.Context, nzblnk string, log func([]byte)) error) *serveState {
	t.Helper()
	oldConf, oldExec, oldCoord := conf, execServeJob, newServeCoordinator
	t.Cleanup(func() { conf, execServeJob, newServeCoordinator = oldConf, oldExec, oldCoord })

	conf.Serve = Serve{SabnzbdPause: false}
	conf.Sabnzbd.Nzbkey = ""
	execServeJob = execFn
	newServeCoordinator = func() sabCoordinator {
		return &sabPauseCoordinator{baseURL: "http://127.0.0.1:0", settle: time.Millisecond, pollWait: time.Second}
	}
	return &serveState{
		jobs:      newJobStore(),
		sessions:  newSessionStore(),
		workQueue: make(chan *serveJob, 16),
	}
}

func waitJobStatus(t *testing.T, s *serveState, id int64, want ...string) *serveJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j := s.jobs.get(id)
		s.jobs.mu.Lock()
		status := j.Status
		s.jobs.mu.Unlock()
		for _, w := range want {
			if status == w {
				return j
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	j := s.jobs.get(id)
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	t.Fatalf("job %d did not reach status %v (status=%q)", id, want, j.Status)
	return nil
}

func TestServeJobLifecycle(t *testing.T) {
	s := testServeState(t, func(ctx context.Context, nzblnk string, log func([]byte)) error {
		log([]byte("scanning for " + nzblnk + "\n"))
		return nil
	})
	go serveWorker(s)

	job := s.jobs.add("nzblnk://?h=abc123")
	s.workQueue <- job

	job = waitJobStatus(t, s, job.ID, "done")
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	if job.Finished.IsZero() {
		t.Fatal("finish time not set")
	}
	if !strings.Contains(string(job.logTail), "scanning for") {
		t.Fatalf("job log missing subprocess output: %q", job.logTail)
	}
}

func TestServeJobFailure(t *testing.T) {
	s := testServeState(t, func(ctx context.Context, nzblnk string, log func([]byte)) error {
		log([]byte("something broke\n"))
		return errors.New("exit status 1")
	})
	go serveWorker(s)

	job := s.jobs.add("nzblnk://?h=abc123")
	s.workQueue <- job

	job = waitJobStatus(t, s, job.ID, "failed")
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	if job.Error != "exit status 1" {
		t.Fatalf("unexpected job error: %q", job.Error)
	}
}

func TestServeJobCancel(t *testing.T) {
	started := make(chan struct{})
	s := testServeState(t, func(ctx context.Context, nzblnk string, log func([]byte)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	go serveWorker(s)

	job := s.jobs.add("nzblnk://?h=abc123")
	s.workQueue <- job
	<-started

	req := httptest.NewRequest(http.MethodDelete, "/api/jobs/"+fmt.Sprint(job.ID), nil)
	req.SetPathValue("id", fmt.Sprint(job.ID))
	rec := httptest.NewRecorder()
	s.handleCancelJob(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel returned %d", rec.Code)
	}
	waitJobStatus(t, s, job.ID, "canceled")
}

func TestServeJobQueueSerialization(t *testing.T) {
	var running atomic.Int32
	var maxConcurrent atomic.Int32
	var orderMu sync.Mutex
	var order []string
	s := testServeState(t, func(ctx context.Context, nzblnk string, log func([]byte)) error {
		cur := running.Add(1)
		for {
			old := maxConcurrent.Load()
			if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		orderMu.Lock()
		order = append(order, nzblnk)
		orderMu.Unlock()
		running.Add(-1)
		return nil
	})
	go serveWorker(s)

	j1 := s.jobs.add("nzblnk://?h=first")
	j2 := s.jobs.add("nzblnk://?h=second")
	s.workQueue <- j1
	s.workQueue <- j2

	waitJobStatus(t, s, j1.ID, "done")
	waitJobStatus(t, s, j2.ID, "done")
	if maxConcurrent.Load() != 1 {
		t.Fatalf("jobs ran concurrently (max=%d)", maxConcurrent.Load())
	}
	if len(order) != 2 || order[0] != "nzblnk://?h=first" || order[1] != "nzblnk://?h=second" {
		t.Fatalf("unexpected execution order: %v", order)
	}
}

func TestServePauseCoordinatorOrdering(t *testing.T) {
	var mu sync.Mutex
	var events []string
	s := testServeState(t, func(ctx context.Context, nzblnk string, log func([]byte)) error {
		mu.Lock()
		events = append(events, "job")
		mu.Unlock()
		return nil
	})
	conf.Serve.SabnzbdPause = true
	s.sabAvailable = true
	searchEngines["directsearch"] = SearchEngine{} // ensure directsearch appears enabled

	newServeCoordinator = func() sabCoordinator {
		return &testCoordinator{events: &events, mu: &mu}
	}
	go serveWorker(s)

	job := s.jobs.add("nzblnk://?h=abc&g=a.b.test&d=1700000000")
	s.workQueue <- job
	waitJobStatus(t, s, job.ID, "done")

	mu.Lock()
	defer mu.Unlock()
	want := []string{"acquire", "job", "release"}
	if len(events) != 3 {
		t.Fatalf("unexpected events: %v", events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("unexpected events: %v", events)
		}
	}
}

type testCoordinator struct {
	events *[]string
	mu     *sync.Mutex
}

func (c *testCoordinator) acquire(ctx context.Context) (func(), error) {
	c.mu.Lock()
	*c.events = append(*c.events, "acquire")
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		*c.events = append(*c.events, "release")
		c.mu.Unlock()
	}, nil
}

func TestServeNoPauseForNonDirectsearch(t *testing.T) {
	var acquired atomic.Bool
	s := testServeState(t, func(ctx context.Context, nzblnk string, log func([]byte)) error {
		return nil
	})
	conf.Serve.SabnzbdPause = true
	s.sabAvailable = true
	delete(searchEngines, "directsearch") // directsearch disabled

	newServeCoordinator = func() sabCoordinator {
		acquired.Store(true)
		return &testCoordinator{events: &[]string{}, mu: &sync.Mutex{}}
	}
	go serveWorker(s)

	job := s.jobs.add("nzblnk://?h=abc")
	s.workQueue <- job
	waitJobStatus(t, s, job.ID, "done")
	if acquired.Load() {
		t.Fatal("SABnzbd was paused for a non-directsearch job")
	}
}

func TestServeHandlers(t *testing.T) {
	s := testServeState(t, func(ctx context.Context, nzblnk string, log func([]byte)) error {
		return nil
	})
	conf.Serve.Token = "test-token"
	s.authRequired = true

	ts := httptest.NewServer(s.routes())
	defer ts.Close()

	// health endpoint is unauthenticated
	resp, err := http.Get(ts.URL + "/api/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz failed: %v %v", resp.StatusCode, err)
	}
	resp.Body.Close()

	// protected route without token -> 401
	resp, err = http.Get(ts.URL + "/api/jobs")
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %v %v", resp.StatusCode, err)
	}
	resp.Body.Close()

	// login with the static token returns a session
	loginResp, err := http.Post(ts.URL+"/api/login", "application/json", strings.NewReader(`{"password":"test-token"}`))
	if err != nil || loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %v %v", loginResp.StatusCode, err)
	}
	var loginData struct {
		Token string `json:"token"`
	}
	json.NewDecoder(loginResp.Body).Decode(&loginData)
	loginResp.Body.Close()
	if loginData.Token == "" {
		t.Fatal("login returned no session token")
	}

	// wrong token is rejected
	badLogin, _ := http.Post(ts.URL+"/api/login", "application/json", strings.NewReader(`{"password":"nope"}`))
	if badLogin.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong token, got %d", badLogin.StatusCode)
	}
	badLogin.Body.Close()

	// session token authorizes job submission
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/jobs", strings.NewReader(`{"nzblnk":"nzblnk://?h=abc123"}`))
	req.Header.Set("Authorization", "Bearer "+loginData.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("job submit failed: %v %v", resp.StatusCode, err)
	}
	var jobData struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&jobData)
	resp.Body.Close()
	if jobData.ID <= 0 {
		t.Fatal("no job id returned")
	}

	// invalid nzblnk -> 400
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/jobs", strings.NewReader(`{"nzblnk":"nzblnk://?t=nope"}`))
	req.Header.Set("Authorization", "Bearer "+loginData.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid nzblnk, got %v %v", resp.StatusCode, err)
	}
	resp.Body.Close()

	// job list contains the job
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+loginData.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("job list failed: %v %v", resp.StatusCode, err)
	}
	var jobs []struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&jobs)
	resp.Body.Close()
	if len(jobs) != 1 || jobs[0].ID != jobData.ID {
		t.Fatalf("unexpected job list: %+v", jobs)
	}

	// static token works as bearer token too
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("static token auth failed: %v %v", resp.StatusCode, err)
	}
	resp.Body.Close()
}

func TestJobUsesDirectsearch(t *testing.T) {
	old := searchEngines
	t.Cleanup(func() { searchEngines = old })

	searchEngines["directsearch"] = SearchEngine{}
	if !jobUsesDirectsearch("nzblnk://?h=abc&g=a.b.test&d=1700000000") {
		t.Fatal("expected directsearch detection for groups+date nzblnk")
	}
	if jobUsesDirectsearch("nzblnk://?h=abc") {
		t.Fatal("false positive for nzblnk without groups/date")
	}
	delete(searchEngines, "directsearch")
	if jobUsesDirectsearch("nzblnk://?h=abc&g=a.b.test&d=1700000000") {
		t.Fatal("directsearch detected while engine disabled")
	}
}

func TestIsLoopbackListen(t *testing.T) {
	for listen, want := range map[string]bool{
		"127.0.0.1:8085": true,
		"localhost:8085": true,
		"[::1]:8085":     true,
		"0.0.0.0:8085":   false,
		"10.0.0.1:8080":  false,
		":8085":          false,
	} {
		if got := isLoopbackListen(listen); got != want {
			t.Errorf("isLoopbackListen(%q) = %v, want %v", listen, got, want)
		}
	}
}
