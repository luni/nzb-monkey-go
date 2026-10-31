package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const validConfig = `
[GENERAL]
target = "EXECUTE,SABNZBD"
categorize = "off"
debug = true
success_wait_time = 5

[SABNZBD]
host = "sab.local"
port = 8080
ssl = true
nzbkey = "key123"
category = "mycat"

[SEARCHENGINES]
nzbindex = 2
easynews = 2
binsearch = 1
directsearch = 0
notanengine = 1

[CATEGORIZER]
movies = "(x264|bluray)"
series = "(s\d+e\d+)"
`

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.conf")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// loadConfig must populate conf correctly: targets, engine ordering
// (ascending weight, alphabetical on ties), categories and debug flag
func TestLoadConfig(t *testing.T) {
	args = struct{ Args }{Args: Args{}}
	confPath = writeConfig(t, validConfig)
	loadConfig()

	if len(conf.General.Targets) != 2 || conf.General.Targets[0] != "EXECUTE" || conf.General.Targets[1] != "SABNZBD" {
		t.Errorf("Targets = %v, want [EXECUTE SABNZBD]", conf.General.Targets)
	}
	if !conf.General.Debug || !args.Debug {
		t.Error("Debug = false, want true")
	}
	if conf.Sabnzbd.Host != "sab.local" || conf.Sabnzbd.Port != 8080 || !conf.Sabnzbd.Ssl || conf.Sabnzbd.Nzbkey != "key123" {
		t.Errorf("unexpected Sabnzbd conf: %+v", conf.Sabnzbd)
	}

	// weights: binsearch=1, easynews=2, nzbindex=2 (tie broken by name),
	// directsearch=0 excluded, unknown engine excluded
	want := []string{"binsearch", "easynews", "nzbindex"}
	if len(conf.Searchengines) != len(want) {
		t.Fatalf("Searchengines = %v, want %v", conf.Searchengines, want)
	}
	for i := range want {
		if conf.Searchengines[i] != want[i] {
			t.Errorf("Searchengines[%d] = %q, want %q", i, conf.Searchengines[i], want[i])
		}
	}

	if len(conf.Categories) != 2 || conf.Categories[0].name != "movies" || conf.Categories[1].name != "series" {
		t.Errorf("Categories = %+v, want movies then series", conf.Categories)
	}

	// defaults must survive for unconfigured directsearch values
	if !conf.Directsearch.SkipCheck {
		t.Error("Directsearch.SkipCheck = false, want default true")
	}
	if conf.Directsearch.Connections != 20 {
		t.Errorf("Directsearch.Connections = %d, want default 20", conf.Directsearch.Connections)
	}
}

// a NEWZNAB entry must register a dynamic engine that resolves through
// SEARCHENGINES weights like a builtin
func TestLoadConfigNewznab(t *testing.T) {
	confPath = writeConfig(t, `
[GENERAL]
target = "EXECUTE"

[SEARCHENGINES]
binsearch = 2
myindexer = 1

[NEWZNAB]
myindexer = "indexer.example.org,KEY-ABC"
broken = "justahost"
unused = "other.example.org,KEY2"
`)
	loadConfig()

	engine, ok := searchEngines["myindexer"]
	if !ok {
		t.Fatal("newznab engine 'myindexer' was not registered")
	}
	wantURL := "https://indexer.example.org/api?t=search&q=%s&apikey=KEY-ABC"
	if engine.searchURL != wantURL {
		t.Errorf("searchURL = %q, want %q", engine.searchURL, wantURL)
	}
	if engine.apikey != "KEY-ABC" {
		t.Errorf("apikey = %q, want KEY-ABC", engine.apikey)
	}
	if engine.search == nil {
		t.Error("search func not set")
	}
	// weight 1 puts it before binsearch (weight 2)
	if len(conf.Searchengines) != 2 || conf.Searchengines[0] != "myindexer" || conf.Searchengines[1] != "binsearch" {
		t.Errorf("Searchengines = %v, want [myindexer binsearch]", conf.Searchengines)
	}
	// the malformed and unused entries must not register
	if _, ok := searchEngines["broken"]; ok {
		t.Error("'broken' newznab entry must not be registered")
	}
	if _, ok := newznabEngines["unused"]; !ok {
		t.Error("valid 'unused' entry should be registered but not enabled")
	}
}

// empty entries in the comma-separated target list must be skipped
func TestLoadConfigEmptyTargetEntries(t *testing.T) {
	confPath = writeConfig(t, `
[GENERAL]
target = "EXECUTE,,SABNZBD,"

[SEARCHENGINES]
binsearch = 1
`)
	loadConfig()

	if len(conf.General.Targets) != 2 || conf.General.Targets[0] != "EXECUTE" || conf.General.Targets[1] != "SABNZBD" {
		t.Errorf("Targets = %v, want [EXECUTE SABNZBD]", conf.General.Targets)
	}
}

// config errors exit with code 1; verified via subprocess
func TestLoadConfigExitPaths(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"unparseable ini", "[unclosed\nkey="},
		{"missing searchengines section", "[GENERAL]\ntarget = \"EXECUTE\"\n"},
		{"no searchengine enabled", "[GENERAL]\ntarget = \"EXECUTE\"\n\n[SEARCHENGINES]\nbinsearch = 0\n"},
		{"no valid targets", "[GENERAL]\ntarget = \"UNKNOWN\"\n\n[SEARCHENGINES]\nbinsearch = 1\n"},
		{"no targets at all", "[GENERAL]\ntarget = \"\"\n\n[SEARCHENGINES]\nbinsearch = 1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, tc.content)
			cmd := exec.Command(os.Args[0], "-test.run=TestLoadConfigHelperProcess")
			cmd.Env = append(os.Environ(),
				"GO_TEST_HELPER=2",
				"TEST_CONFPATH="+path,
			)
			err := cmd.Run()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("expected exit code 1, got %v", err)
			}
		})
	}
}

// helper process for loadConfig exit tests; not a real test when run by the parent
func TestLoadConfigHelperProcess(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "2" {
		return
	}
	conf.General.Error_wait_time = 1 // keep the exit countdown short
	confPath = os.Getenv("TEST_CONFPATH")
	loadConfig()
	os.Exit(0) // loadConfig must not return on failure paths
}

// an existing regular config file must not trigger creation or
// registration - checkForConfig returns without side effects
func TestCheckForConfigExists(t *testing.T) {
	confPath = writeConfig(t, validConfig)
	checkForConfig() // must not exit or prompt
}
