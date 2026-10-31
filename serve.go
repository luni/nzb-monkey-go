package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed assets/serve.html
var servePage []byte

const (
	serveMaxJobs     = 100
	serveMaxLogBytes = 256 * 1024
	serveSessionTTL  = 24 * time.Hour
)

type serveJob struct {
	ID       int64     `json:"id"`
	Nzblnk   string    `json:"nzblnk"`
	Status   string    `json:"status"` // queued, running, done, failed, canceled
	Created  time.Time `json:"created"`
	Finished time.Time `json:"finished,omitempty"`
	Error    string    `json:"error,omitempty"`
	logTail  []byte
	cancel   context.CancelFunc
}

type jobStore struct {
	mu     sync.Mutex
	jobs   map[int64]*serveJob
	order  []int64
	nextID int64
}

func newJobStore() *jobStore {
	return &jobStore{jobs: make(map[int64]*serveJob)}
}

func (s *jobStore) add(nzblnk string) *serveJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	job := &serveJob{ID: s.nextID, Nzblnk: nzblnk, Status: "queued", Created: time.Now()}
	s.jobs[job.ID] = job
	s.order = append(s.order, job.ID)
	for len(s.order) > serveMaxJobs {
		delete(s.jobs, s.order[0])
		s.order = s.order[1:]
	}
	return job
}

func (s *jobStore) get(id int64) *serveJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[id]
}

func (s *jobStore) list() []*serveJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*serveJob, 0, len(s.order))
	for _, id := range s.order {
		if j := s.jobs[id]; j != nil {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

func (j *serveJob) appendLog(p []byte) {
	// caller holds jobStore lock via logWriter wrapper
	if len(j.logTail)+len(p) > serveMaxLogBytes {
		j.logTail = append(j.logTail[len(p):], p...)
		if len(j.logTail) > serveMaxLogBytes {
			j.logTail = j.logTail[len(j.logTail)-serveMaxLogBytes:]
		}
		return
	}
	j.logTail = append(j.logTail, p...)
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]time.Time)}
}

func (s *sessionStore) issue() string {
	b := make([]byte, 16)
	rand.Read(b)
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(serveSessionTTL)
	// opportunistic cleanup
	for k, exp := range s.sessions {
		if time.Now().After(exp) {
			delete(s.sessions, k)
		}
	}
	s.mu.Unlock()
	return token
}

func (s *sessionStore) valid(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sessions[token]
	return ok && time.Now().Before(exp)
}

// serveState wires the HTTP handlers to the job/session stores and config.
type serveState struct {
	jobs         *jobStore
	sessions     *sessionStore
	workQueue    chan *serveJob
	sabLogin     bool   // SABnzbd has web UI login enabled
	sabLoginURL  string // SABnzbd web UI login endpoint
	sabAvailable bool   // SABnzbd API reachable at startup
	authRequired bool   // any bearer token required
}

// serve starts the HTTP service. It blocks forever; on error it logs and
// returns so the caller can exit with a failure code.
func serve() {
	state := &serveState{
		jobs:      newJobStore(),
		sessions:  newSessionStore(),
		workQueue: make(chan *serveJob, 16),
	}

	// probe whether the SABnzbd web interface has its own login
	if conf.Sabnzbd.Host != "" && conf.Sabnzbd.Nzbkey != "" {
		loginURL, err := sabnzbdLoginURL(sabnzbdBaseURL())
		if err != nil {
			Log.Warn("Could not probe SABnzbd web login: %v - login form disabled", err)
		} else {
			state.sabAvailable = true
			state.sabLogin = loginURL != ""
			state.sabLoginURL = loginURL
			if state.sabLogin {
				Log.Debug("SABnzbd web login detected at %s", loginURL)
			}
		}
		// un-stick a leftover queue pause from a previous run, best effort
		if conf.Serve.SabnzbdPause {
			if err := sabnzbdSimpleCall(sabnzbdBaseURL(), conf.Sabnzbd.Nzbkey, "resume"); err != nil {
				Log.Debug("SABnzbd resume at startup skipped: %v", err)
			}
		}
	}
	state.authRequired = conf.Serve.SabnzbdAuth && state.sabLogin || conf.Serve.Token != ""
	if !state.authRequired && !isLoopbackListen(conf.Serve.Listen) {
		Log.Warn("Service is listening on a non-loopback address without authentication")
	}

	go serveWorker(state)

	ln, err := net.Listen("tcp", conf.Serve.Listen)
	if err != nil {
		Log.Error("Failed to listen on %s: %v", conf.Serve.Listen, err)
		exit(1)
	}
	Log.Info("Serving nzblnk web interface on http://%s/", conf.Serve.Listen)
	if err := http.Serve(ln, state.routes()); err != nil {
		Log.Error("HTTP server stopped: %v", err)
		exit(1)
	}
}

func (s *serveState) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /api/healthz", s.handleHealthz)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("GET /api/authz", s.handleAuthz)
	mux.HandleFunc("POST /api/jobs", s.withAuth(s.handleSubmitJob))
	mux.HandleFunc("GET /api/jobs", s.withAuth(s.handleListJobs))
	mux.HandleFunc("GET /api/jobs/{id}", s.withAuth(s.handleGetJob))
	mux.HandleFunc("GET /api/jobs/{id}/log", s.withAuth(s.handleJobLog))
	mux.HandleFunc("DELETE /api/jobs/{id}", s.withAuth(s.handleCancelJob))
	return mux
}

func isLoopbackListen(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil || host == "" {
		return false
	}
	return net.ParseIP(host).IsLoopback() || host == "localhost"
}

func (s *serveState) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authRequired {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if token == r.Header.Get("Authorization") {
				token = r.URL.Query().Get("token")
			}
			if token == "" || !(s.sessions.valid(token) || token == conf.Serve.Token && conf.Serve.Token != "") {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
				return
			}
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (s *serveState) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(servePage)
}

func (s *serveState) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handleAuthz reports which auth flow the page should offer.
func (s *serveState) handleAuthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"required":   s.authRequired,
		"sab_login":  s.sabLogin,
		"sab_online": s.sabAvailable,
	})
}

func (s *serveState) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
		return
	}
	ok := false
	if s.sabLogin {
		var err error
		ok, err = sabnzbdLogin(s.sabLoginURL, req.Username, req.Password)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "SABnzbd auth unavailable"})
			return
		}
	} else if conf.Serve.Token != "" {
		ok = req.Password == conf.Serve.Token
	}
	if !ok {
		time.Sleep(500 * time.Millisecond) // slow down credential guessing
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid credentials"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": s.sessions.issue()})
}

func (s *serveState) handleSubmitJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nzblnk string `json:"nzblnk"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Nzblnk) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "nzblnk required"})
		return
	}
	// validate by parsing the nzblnk like the CLI does
	link, err := parseNzblnk(strings.TrimSpace(req.Nzblnk))
	if err != nil || link.header == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid nzblnk: missing 'h' parameter"})
		return
	}
	job := s.jobs.add(strings.TrimSpace(req.Nzblnk))
	select {
	case s.workQueue <- job:
	default:
		s.finishJob(job, "failed", "job queue is full")
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "job queue is full"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": job.ID})
}

func (s *serveState) handleListJobs(w http.ResponseWriter, r *http.Request) {
	type jobJSON struct {
		ID       int64     `json:"id"`
		Nzblnk   string    `json:"nzblnk"`
		Status   string    `json:"status"`
		Created  time.Time `json:"created"`
		Finished time.Time `json:"finished,omitempty"`
		Error    string    `json:"error,omitempty"`
	}
	out := []jobJSON{}
	for _, j := range s.jobs.list() {
		s.jobs.mu.Lock()
		out = append(out, jobJSON{j.ID, j.Nzblnk, j.Status, j.Created, j.Finished, j.Error})
		s.jobs.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *serveState) handleGetJob(w http.ResponseWriter, r *http.Request) {
	var id int64
	fmt.Sscanf(r.PathValue("id"), "%d", &id)
	j := s.jobs.get(id)
	if j == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"id": j.ID, "nzblnk": j.Nzblnk, "status": j.Status,
		"created": j.Created, "finished": j.Finished, "error": j.Error,
		"log": string(j.logTail),
	})
}

func (s *serveState) handleJobLog(w http.ResponseWriter, r *http.Request) {
	var id int64
	fmt.Sscanf(r.PathValue("id"), "%d", &id)
	j := s.jobs.get(id)
	if j == nil {
		http.NotFound(w, r)
		return
	}
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(j.logTail)
}

func (s *serveState) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	var id int64
	fmt.Sscanf(r.PathValue("id"), "%d", &id)
	j := s.jobs.get(id)
	if j == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	if j.Status == "running" || j.Status == "queued" {
		if j.cancel != nil {
			j.cancel()
		}
		j.Status = "canceled"
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": j.Status})
}

// serveWorker drains the job queue one job at a time.
func serveWorker(s *serveState) {
	for job := range s.workQueue {
		runServeJob(s, job)
	}
}

// jobUsesDirectsearch reports whether the job can end up on the NNTP direct
// search (directsearch enabled and the nzblnk carries groups + date).
func jobUsesDirectsearch(nzblnk string) bool {
	if _, ok := searchEngines["directsearch"]; !ok {
		return false
	}
	link, err := parseNzblnk(nzblnk)
	if err != nil {
		return false
	}
	return len(link.groups) > 0 && link.date != ""
}

func runServeJob(s *serveState, job *serveJob) {
	s.jobs.mu.Lock()
	job.Status = "running"
	s.jobs.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	s.jobs.mu.Lock()
	job.cancel = cancel
	s.jobs.mu.Unlock()
	defer cancel()

	var release func()
	if conf.Serve.SabnzbdPause && s.sabAvailable && jobUsesDirectsearch(job.Nzblnk) {
		var err error
		release, err = newServeCoordinator().acquire(ctx)
		if err != nil {
			s.finishJob(job, "failed", fmt.Sprintf("failed to pause SABnzbd: %v", err))
			return
		}
		s.appendJobLog(job, "SABnzbd queue paused for direct search\n")
	}
	if release != nil {
		defer release()
		defer s.appendJobLog(job, "SABnzbd queue resumed\n")
	}

	werr := execServeJob(ctx, job.Nzblnk, func(p []byte) {
		s.appendJobLog(job, string(p))
	})

	s.jobs.mu.Lock()
	if job.Status == "canceled" {
		s.jobs.mu.Unlock()
		return
	}
	s.jobs.mu.Unlock()

	if ctx.Err() != nil {
		s.finishJob(job, "canceled", "")
		return
	}
	if werr != nil {
		s.finishJob(job, "failed", werr.Error())
		return
	}
	s.finishJob(job, "done", "")
}

// newServeCoordinator builds the SABnzbd coordinator for a job. It is a
// variable so tests can stub the coordinator.
var newServeCoordinator = func() sabCoordinator {
	return newSabPauseCoordinator()
}

// execServeJob runs one nzblnk through the regular CLI pipeline in a
// subprocess. It is a variable so tests can stub the process boundary.
var execServeJob = func(ctx context.Context, nzblnk string, log func([]byte)) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, exe, "--config", confPath, nzblnk)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout // merge into the captured log

	if err := cmd.Start(); err != nil {
		return err
	}
	buf := make([]byte, 32*1024)
	for {
		n, rerr := out.Read(buf)
		if n > 0 {
			log(buf[:n])
		}
		if rerr != nil {
			break
		}
	}
	return cmd.Wait()
}

func (s *serveState) appendJobLog(job *serveJob, text string) {
	s.jobs.mu.Lock()
	job.appendLog([]byte(text))
	s.jobs.mu.Unlock()
}

func (s *serveState) finishJob(job *serveJob, status string, errText string) {
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	job.Status = status
	job.Error = errText
	job.Finished = time.Now()
}
