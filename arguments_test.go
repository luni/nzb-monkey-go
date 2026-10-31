package main

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	parser "github.com/alexflint/go-arg"
)

func resetArgs(a Args) {
	args = struct{ Args }{Args: a}
	argParser, _ = parser.NewParser(parser.Config{IgnoreEnv: true}, &args)
}

// checkArguments must succeed for a NZBLNK URI alone
func TestCheckArgumentsNzblnk(t *testing.T) {
	resetArgs(Args{Nzblnk: "nzblnk://?h=some+header&t=some+title&p=secret&g=alt.binaries.test&d=1234567890"})
	checkArguments()

	if args.Header != "some header" {
		t.Errorf("Header = %q, want %q", args.Header, "some header")
	}
	if args.Title != "some title" {
		t.Errorf("Title = %q, want %q", args.Title, "some title")
	}
	if args.Password != "secret" {
		t.Errorf("Password = %q, want %q", args.Password, "secret")
	}
	if len(args.Groups) != 1 || args.Groups[0] != "alt.binaries.test" {
		t.Errorf("Groups = %v, want [alt.binaries.test]", args.Groups)
	}
	if args.UnixDate != 1234567890 || !args.IsTimestamp {
		t.Errorf("UnixDate = %d (IsTimestamp=%v), want 1234567890 (true)", args.UnixDate, args.IsTimestamp)
	}
}

// CLI arguments take precedence over NZBLNK parameters (regression test:
// previously this combination wrongly exited with "missing 'h' parameter")
func TestCheckArgumentsCliPrecedence(t *testing.T) {
	resetArgs(Args{
		Nzblnk: "nzblnk://?h=nzblnk+header&t=nzblnk+title&p=nzblnkpass",
		Header: "cli header",
		Title:  "cli title",
	})
	checkArguments()

	if args.Header != "cli header" {
		t.Errorf("Header = %q, want %q (CLI must win)", args.Header, "cli header")
	}
	if args.Title != "cli title" {
		t.Errorf("Title = %q, want %q (CLI must win)", args.Title, "cli title")
	}
	if args.Password != "nzblnkpass" {
		t.Errorf("Password = %q, want %q (from NZBLNK)", args.Password, "nzblnkpass")
	}
}

// title falls back to header when not provided
func TestCheckArgumentsTitleFallback(t *testing.T) {
	resetArgs(Args{Header: "my subject"})
	checkArguments()

	if args.Title != "my subject" {
		t.Errorf("Title = %q, want %q", args.Title, "my subject")
	}
}

// groups from NZBLNK are split on commas, semicolons and spaces
// (note: semicolons must be percent-encoded in a valid NZBLNK URI;
// empty g values must be skipped, not produce empty groups)
func TestCheckArgumentsGroupSplitting(t *testing.T) {
	resetArgs(Args{Nzblnk: "nzblnk://?h=h&g=&g=alt.binaries.a,,alt.binaries.b%3Balt.binaries.c"})
	checkArguments()

	want := []string{"alt.binaries.a", "alt.binaries.b", "alt.binaries.c"}
	if len(args.Groups) != len(want) {
		t.Fatalf("Groups = %v, want %v", args.Groups, want)
	}
	for i := range want {
		if args.Groups[i] != want[i] {
			t.Errorf("Groups[%d] = %q, want %q", i, args.Groups[i], want[i])
		}
	}
}

// a real-world NZBLNK: no // after the scheme, repeated g params,
// hashed header and unix timestamp
func TestCheckArgumentsRealWorldNzblnk(t *testing.T) {
	resetArgs(Args{Nzblnk: "nzblnk:?t=Some.Title.2024.German.DOKU.1080p.WEB.x264-GROUP&h=e86bfa21b92d1655f425d4df&p=0kU73jAkRwHaRdXzD8oIBIlJsTylEl1Y4zrpT1W&g=a.b.a51&g=a.b.jph&g=a.b.frogs&d=1738010154"})
	checkArguments()

	if args.Header != "e86bfa21b92d1655f425d4df" {
		t.Errorf("Header = %q, want hashed header", args.Header)
	}
	if args.Title != "Some.Title.2024.German.DOKU.1080p.WEB.x264-GROUP" {
		t.Errorf("Title = %q, want dotted release name", args.Title)
	}
	want := []string{"alt.binaries.a51", "alt.binaries.jph", "alt.binaries.frogs"}
	if len(args.Groups) != len(want) {
		t.Fatalf("Groups = %v, want %v", args.Groups, want)
	}
	for i := range want {
		if args.Groups[i] != want[i] {
			t.Errorf("Groups[%d] = %q, want %q", i, args.Groups[i], want[i])
		}
	}
	if args.UnixDate != 1738010154 || !args.IsTimestamp {
		t.Errorf("UnixDate = %d (IsTimestamp=%v), want 1738010154 (true)", args.UnixDate, args.IsTimestamp)
	}
}

// the a.b. abbreviation is expanded as a prefix only
func TestCheckArgumentsGroupPrefixExpansion(t *testing.T) {
	resetArgs(Args{Header: "h", Groups: []string{"a.b.test", "alt.binaries.ok", "misc.a.b.mid"}})
	checkArguments()

	want := []string{"alt.binaries.test", "alt.binaries.ok", "misc.a.b.mid"}
	for i := range want {
		if args.Groups[i] != want[i] {
			t.Errorf("Groups[%d] = %q, want %q", i, args.Groups[i], want[i])
		}
	}
}

// date in DD.MM.YYYY format parses and is not flagged as timestamp
func TestCheckArgumentsDateFormat(t *testing.T) {
	resetArgs(Args{Header: "h", Date: "14.02.2009"})
	checkArguments()

	if args.UnixDate == 0 {
		t.Error("UnixDate = 0, want a parsed timestamp")
	}
	if args.IsTimestamp {
		t.Error("IsTimestamp = true, want false for DD.MM.YYYY input")
	}
}

// an invalid --date value from the CLI must fail, unlike an invalid
// NZBLNK 'd' parameter which only warns
func TestCheckArgumentsInvalidCliDate(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestCheckArgumentsHelperProcess")
	cmd.Env = append(os.Environ(),
		"GO_TEST_HELPER=1",
		"TEST_NZBLNK=",
		"TEST_SUBJECT=header",
		"TEST_DATE=garbage",
	)
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, got %v", err)
	}
}

// an invalid 'd' parameter in a NZBLNK warns but does not fail
func TestCheckArgumentsInvalidNzblnkDate(t *testing.T) {
	resetArgs(Args{Nzblnk: "nzblnk://?h=header&d=garbage"})
	checkArguments()

	if args.Header != "header" {
		t.Errorf("Header = %q, want %q", args.Header, "header")
	}
	if args.UnixDate != 0 {
		t.Errorf("UnixDate = %d, want 0 for invalid date", args.UnixDate)
	}
}

// failure paths call exit(1) which terminates via os.Exit; verify via subprocess
func TestCheckArgumentsExitPaths(t *testing.T) {
	cases := []struct {
		name    string
		nzblnk  string
		subject string
	}{
		{"no header and no nzblnk", "", ""},
		{"nzblnk missing h param", "nzblnk://?t=only", ""},
		{"nzblnk query unparseable", "nzblnk://?%zz", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=TestCheckArgumentsHelperProcess")
			cmd.Env = append(os.Environ(),
				"GO_TEST_HELPER=1",
				"TEST_NZBLNK="+tc.nzblnk,
				"TEST_SUBJECT="+tc.subject,
			)
			err := cmd.Run()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("expected exit code 1, got %v", err)
			}
		})
	}
}

// helper process for exit path tests; not a real test when run by the parent
func TestCheckArgumentsHelperProcess(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	argParser, _ = parser.NewParser(parser.Config{IgnoreEnv: true}, &args)
	conf.General.Error_wait_time = 1 // keep the exit countdown short
	args.Nzblnk = os.Getenv("TEST_NZBLNK")
	args.Header = os.Getenv("TEST_SUBJECT")
	args.Date = os.Getenv("TEST_DATE")
	checkArguments()
	os.Exit(0) // checkArguments must not return on failure paths
}
