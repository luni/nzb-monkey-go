package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// set up conf.Nzbget pointing at an httptest server
func nzbgetTestConf(t *testing.T, srv *httptest.Server) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	conf.Nzbget = NZBGet{
		Host:              u.Hostname(),
		Port:              port,
		BasicauthUsername: "user",
		BasicauthPassword: "pass",
	}
}

// getCategories must return only exact "CategoryN.Name" entries
func TestNzbgetGetCategories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")) {
			t.Errorf("Authorization = %q, want Basic auth header", got)
		}
		if r.URL.Path != "/jsonrpc/config" {
			t.Errorf("path = %q, want /jsonrpc/config", r.URL.Path)
		}
		fmt.Fprint(w, `{"version":"1.1","result":[
			{"Name":"Category1.Name","Value":"movies"},
			{"Name":"Category1.Unpack","Value":"yes"},
			{"Name":"FooCategory2.Name","Value":"decoy"},
			{"Name":"Category10.Name","Value":"series"},
			{"Name":"Other.Option","Value":"x"}
		]}`)
	}))
	defer srv.Close()
	nzbgetTestConf(t, srv)

	categories, err := nzbget_getCategories()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"movies", "series"}
	if len(categories) != len(want) {
		t.Fatalf("categories = %v, want %v", categories, want)
	}
	for i := range want {
		if categories[i] != want[i] {
			t.Errorf("categories[%d] = %q, want %q", i, categories[i], want[i])
		}
	}
}

func TestNzbgetGetCategoriesEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"result":[]}`)
	}))
	defer srv.Close()
	nzbgetTestConf(t, srv)

	if _, err := nzbget_getCategories(); err == nil {
		t.Error("expected error for empty result, got nil")
	}
}

// push must send the JSON-RPC append call with base64 NZB and password;
// result > 0 means success
func TestNzbgetPush(t *testing.T) {
	var params []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		if body.Method != "append" {
			t.Errorf("method = %q, want append", body.Method)
		}
		params = body.Params
		fmt.Fprint(w, `{"version":"1.1","result":123}`)
	}))
	defer srv.Close()
	nzbgetTestConf(t, srv)

	args.Title = "test title"
	args.Category = ""
	args.Password = "secret"
	conf.Nzbget.Addpaused = true
	conf.Nzbget.Category = ""

	if err := nzbget_push("<nzb>data</nzb>", "movies"); err != nil {
		t.Fatalf("push: %v", err)
	}

	if len(params) != 10 {
		t.Fatalf("params = %v, want 10 entries", params)
	}
	if params[0] != "test title.nzb" {
		t.Errorf("params[0] = %v, want filename", params[0])
	}
	if decoded, err := base64.StdEncoding.DecodeString(params[1].(string)); err != nil || string(decoded) != "<nzb>data</nzb>" {
		t.Errorf("params[1] does not decode to the nzb: %v (%v)", params[1], err)
	}
	if params[2] != "movies" {
		t.Errorf("params[2] = %v, want category movies", params[2])
	}
	if params[5] != true {
		t.Errorf("params[5] = %v, want AddPaused true", params[5])
	}
	if pp, ok := params[9].(map[string]any); !ok || pp["*unpack:password"] != "secret" {
		t.Errorf("params[9] = %v, want unpack password", params[9])
	}
}

// a non-positive result must be reported as an error
func TestNzbgetPushError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"version":"1.1","result":0}`)
	}))
	defer srv.Close()
	nzbgetTestConf(t, srv)

	args.Title = "test"
	args.Category = ""
	args.Password = ""

	if err := nzbget_push("<nzb/>", ""); err == nil {
		t.Error("expected error, got nil")
	}
}
