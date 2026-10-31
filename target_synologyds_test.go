package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestSynologydsCheckError(t *testing.T) {
	if err := synologyds_checkError(400); err == nil || !strings.Contains(err.Error(), "incorrect password") {
		t.Errorf("checkError(400) = %v, want 'incorrect password' text", err)
	}
	if err := synologyds_checkError(999); err == nil || !strings.Contains(err.Error(), "Unknown error") {
		t.Errorf("checkError(999) = %v, want fallback 'Unknown error'", err)
	}
	if err := synologyds_checkError(100); err == nil || !strings.Contains(err.Error(), "Unknown error") {
		t.Errorf("checkError(100) = %v, want 'Unknown error'", err)
	}
}

// set up conf.Synologyds pointing at an httptest server
func synologydsTestConf(t *testing.T, srv *httptest.Server) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	conf.Synologyds = SynologyDS{
		Host:     u.Hostname(),
		Port:     port,
		Username: "user",
		Password: "pass",
	}
}

// a successful API info + login exchange returns a sid (regression test:
// the previous implementation panicked on unexpected response shapes)
func TestSynologydsAuthenticateSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api") == "SYNO.API.Info" {
			fmt.Fprint(w, `{"success":true,"data":{
				"SYNO.DownloadStation2.Task":{"path":"task.cgi","minVersion":1,"maxVersion":2},
				"SYNO.API.Auth":{"path":"auth.cgi","maxVersion":6}
			}}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"sid":"test-sid-123"}}`)
	}))
	defer srv.Close()
	synologydsTestConf(t, srv)

	options, err := synologyds_authenticate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if options.sid != "test-sid-123" {
		t.Errorf("sid = %q, want %q", options.sid, "test-sid-123")
	}
	if options.path != "task.cgi" || options.maxVersion != 2 {
		t.Errorf("path/maxVersion = %q/%d, want task.cgi/2", options.path, options.maxVersion)
	}
}

// malformed API responses must return errors, not panic
func TestSynologydsAuthenticateMalformed(t *testing.T) {
	cases := []struct {
		name     string
		infoBody string
	}{
		{"task info not an object", `{"success":true,"data":{"SYNO.DownloadStation2.Task":"oops"}}`},
		{"missing path field", `{"success":true,"data":{"SYNO.DownloadStation2.Task":{"minVersion":1,"maxVersion":2},"SYNO.API.Auth":{"path":"a","maxVersion":1}}}`},
		{"auth info not an object", `{"success":true,"data":{"SYNO.DownloadStation2.Task":{"path":"t","minVersion":1,"maxVersion":2},"SYNO.API.Auth":"oops"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.infoBody)
			}))
			defer srv.Close()
			synologydsTestConf(t, srv)

			if _, err := synologyds_authenticate(); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// a failed login must surface the Synology error code, not "unknown response"
func TestSynologydsAuthenticateLoginFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api") == "SYNO.API.Info" {
			fmt.Fprint(w, `{"success":true,"data":{
				"SYNO.DownloadStation2.Task":{"path":"task.cgi","minVersion":1,"maxVersion":2},
				"SYNO.API.Auth":{"path":"auth.cgi","maxVersion":6}
			}}`)
			return
		}
		// login failure: wrong password (error code 400)
		fmt.Fprint(w, `{"success":false,"error":{"code":400}}`)
	}))
	defer srv.Close()
	synologydsTestConf(t, srv)

	_, err := synologyds_authenticate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error = %q, want it to contain error code 400", err.Error())
	}
}

// full push flow: authenticate then upload; the extract_password field
// must contain the escaped password (regression test: a " in the password
// produced malformed JSON in the field value)
func TestSynologydsPush(t *testing.T) {
	var mu sync.Mutex
	var form map[string][]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("ParseMultipartForm: %v", err)
			}
			mu.Lock()
			form = r.MultipartForm.Value
			mu.Unlock()
			fmt.Fprint(w, `{"success":true}`)
		case r.URL.Query().Get("api") == "SYNO.API.Info":
			fmt.Fprint(w, `{"success":true,"data":{
				"SYNO.DownloadStation2.Task":{"path":"task.cgi","minVersion":1,"maxVersion":2},
				"SYNO.API.Auth":{"path":"auth.cgi","maxVersion":6}
			}}`)
		default:
			fmt.Fprint(w, `{"success":true,"data":{"sid":"test-sid-123"}}`)
		}
	}))
	defer srv.Close()
	synologydsTestConf(t, srv)

	args.Title = "test title"
	args.Password = `esc"ape\me`

	if err := synologyds_push("<nzb>data</nzb>", ""); err != nil {
		t.Fatalf("push: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if form == nil {
		t.Fatal("server never received the multipart upload")
	}
	if got := form["extract_password"]; len(got) != 1 || got[0] != `"esc\"ape\\me"` {
		t.Errorf("extract_password = %v, want escaped %q", got, `"esc\"ape\\me"`)
	}
	if got := form["method"]; len(got) != 1 || got[0] != "create" {
		t.Errorf("method = %v, want [create]", got)
	}
	if got := form["size"]; len(got) != 1 || got[0] != "15" {
		t.Errorf("size = %v, want [15]", got)
	}
}

// an API error response on upload must surface the Synology error code
func TestSynologydsPushError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			fmt.Fprint(w, `{"success":false,"error":{"code":108}}`)
		case r.URL.Query().Get("api") == "SYNO.API.Info":
			fmt.Fprint(w, `{"success":true,"data":{
				"SYNO.DownloadStation2.Task":{"path":"task.cgi","minVersion":1,"maxVersion":2},
				"SYNO.API.Auth":{"path":"auth.cgi","maxVersion":6}
			}}`)
		default:
			fmt.Fprint(w, `{"success":true,"data":{"sid":"test-sid-123"}}`)
		}
	}))
	defer srv.Close()
	synologydsTestConf(t, srv)

	args.Title = "test"
	args.Password = ""

	err := synologyds_push("<nzb>data</nzb>", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "108") {
		t.Errorf("error = %q, want it to contain code 108", err.Error())
	}
}
