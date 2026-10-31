package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// set up conf.Sabnzbd pointing at an httptest server, including a base
// path to exercise the URL joining in request()
func sabnzbdTestConf(t *testing.T, srv *httptest.Server) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	conf.Sabnzbd = SABnzbd{
		Host:     u.Hostname(),
		Port:     port,
		Basepath: "/sab/",
		Nzbkey:   "key123",
	}
}

// getCategories must drop the first entry (SABnzbd's default category)
func TestSabnzbdGetCategories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sab/api" {
			t.Errorf("path = %q, want /sab/api (basepath joined)", r.URL.Path)
		}
		if r.URL.Query().Get("apikey") != "key123" || r.URL.Query().Get("mode") != "get_cats" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"categories":["Default","movies","series"]}`)
	}))
	defer srv.Close()
	sabnzbdTestConf(t, srv)

	categories, err := sabnzbd_getCategories()
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

// a single-entry response (only the default) must return no categories
func TestSabnzbdGetCategoriesDefaultOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"categories":["Default"]}`)
	}))
	defer srv.Close()
	sabnzbdTestConf(t, srv)

	categories, err := sabnzbd_getCategories()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(categories) != 0 {
		t.Errorf("categories = %v, want empty", categories)
	}
}

// push must send the multipart upload; status:true + nzo_ids means success
func TestSabnzbdPush(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("mode") != "addfile" || q.Get("apikey") != "key123" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		if q.Get("priority") != "-2" {
			t.Errorf("priority = %q, want -2 (Addpaused)", q.Get("priority"))
		}
		if q.Get("cat") != "movies" {
			t.Errorf("cat = %q, want movies", q.Get("cat"))
		}
		fmt.Fprint(w, `{"status":true,"nzo_ids":["SABnzbd_nzo_1"]}`)
	}))
	defer srv.Close()
	sabnzbdTestConf(t, srv)

	args.Title = "test title"
	args.Category = ""
	args.Password = ""
	conf.Sabnzbd.Addpaused = true
	conf.Sabnzbd.Category = "movies"
	conf.Sabnzbd.Compression = "none"

	if err := sabnzbd_push("<nzb>data</nzb>", ""); err != nil {
		t.Fatalf("push: %v", err)
	}
}

// status:false must be reported as an error
func TestSabnzbdPushError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":false}`)
	}))
	defer srv.Close()
	sabnzbdTestConf(t, srv)

	args.Title = "test"
	args.Category = ""
	args.Password = ""
	conf.Sabnzbd.Category = ""
	conf.Sabnzbd.Compression = "none"

	if err := sabnzbd_push("<nzb/>", ""); err == nil {
		t.Error("expected error, got nil")
	}
}
