package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// sabnzbdApiGet performs a GET against the SABnzbd API and returns the
// decoded JSON body.
func sabnzbdApiGet(baseURL string, query url.Values) (map[string]any, error) {
	u := strings.TrimRight(baseURL, "/") + "/api?" + query.Encode()
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("unexpected response from SABnzbd: %v", err)
	}
	if msg, ok := decoded["error"].(string); ok && msg != "" {
		return nil, fmt.Errorf("SABnzbd API error: %s", msg)
	}
	return decoded, nil
}

// sabnzbdSimpleCall calls a queue-affecting mode and checks the status flag.
func sabnzbdSimpleCall(baseURL string, apikey string, mode string) error {
	query := url.Values{"mode": {mode}, "output": {"json"}, "apikey": {apikey}}
	res, err := sabnzbdApiGet(baseURL, query)
	if err != nil {
		return err
	}
	if status, ok := res["status"].(bool); ok && status {
		return nil
	}
	return fmt.Errorf("SABnzbd mode=%s did not return status true", mode)
}

// sabnzbdAuthMethods returns the auth methods SABnzbd reports (e.g. apikey,
// login, cert).
func sabnzbdAuthMethods(baseURL string) (map[string]bool, error) {
	res, err := sabnzbdApiGet(baseURL, url.Values{"mode": {"auth"}, "output": {"json"}})
	if err != nil {
		return nil, err
	}
	methods := make(map[string]bool)
	switch auth := res["auth"].(type) {
	case string:
		methods[auth] = true
	case []any:
		for _, m := range auth {
			if s, ok := m.(string); ok {
				methods[s] = true
			}
		}
	case map[string]any:
		for k, v := range auth {
			if b, ok := v.(bool); ok && b {
				methods[k] = true
			}
		}
	}
	return methods, nil
}

// sabnzbdQueuePaused reports whether the SABnzbd queue is paused.
func sabnzbdQueuePaused(baseURL string, apikey string) (bool, error) {
	query := url.Values{"mode": {"queue"}, "output": {"json"}, "apikey": {apikey}}
	res, err := sabnzbdApiGet(baseURL, query)
	if err != nil {
		return false, err
	}
	if queue, ok := res["queue"].(map[string]any); ok {
		if paused, ok := queue["paused"].(bool); ok {
			return paused, nil
		}
	}
	return false, fmt.Errorf("could not determine SABnzbd queue state")
}

// sabnzbdLogin verifies web UI credentials against SABnzbd's /login/ page.
// It only applies when SABnzbd itself has web interface login enabled.
func sabnzbdLogin(baseURL string, username string, password string) (bool, error) {
	form := url.Values{"username": {username}, "password": {password}}
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // inspect the redirect response itself
		},
	}
	resp, err := client.PostForm(strings.TrimRight(baseURL, "/")+"/login/", form)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	// successful login redirects to the interface and sets the login cookie
	for _, c := range resp.Cookies() {
		if c.Name == "login_cookie" && c.Value != "" {
			return true, nil
		}
	}
	return false, nil
}

// sabCoordinator controls SABnzbd around directsearch jobs so the shared
// usenet connection budget is not exceeded.
type sabCoordinator interface {
	// acquire pauses SABnzbd's queue and returns a release function that
	// resumes it. release is always safe to call, including on error paths.
	acquire(ctx context.Context) (release func(), err error)
}

// sabPauseCoordinator implements sabCoordinator via mode=pause/resume.
type sabPauseCoordinator struct {
	baseURL  string
	apikey   string
	settle   time.Duration
	pollWait time.Duration
}

func newSabPauseCoordinator() *sabPauseCoordinator {
	return &sabPauseCoordinator{
		baseURL:  sabnzbdBaseURL(),
		apikey:   conf.Sabnzbd.Nzbkey,
		settle:   2 * time.Second,
		pollWait: 15 * time.Second,
	}
}

func (c *sabPauseCoordinator) acquire(ctx context.Context) (func(), error) {
	if err := sabnzbdSimpleCall(c.baseURL, c.apikey, "pause"); err != nil {
		return nil, err
	}
	// wait until the queue reports paused so in-flight jobs settle
	deadline := time.Now().Add(c.pollWait)
	for {
		paused, err := sabnzbdQueuePaused(c.baseURL, c.apikey)
		if err == nil && paused {
			break
		}
		if time.Now().After(deadline) {
			break // pause was accepted - proceed even if the state is unclear
		}
		select {
		case <-ctx.Done():
			sabnzbdSimpleCall(c.baseURL, c.apikey, "resume")
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	// give running connections a moment to drain
	select {
	case <-ctx.Done():
		sabnzbdSimpleCall(c.baseURL, c.apikey, "resume")
		return nil, ctx.Err()
	case <-time.After(c.settle):
	}
	return func() {
		if err := sabnzbdSimpleCall(c.baseURL, c.apikey, "resume"); err != nil {
			Log.Warn("Failed to resume SABnzbd queue: %v", err)
		}
	}, nil
}
