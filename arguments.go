package main

import (
	"bufio"
	"bytes"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	parser "github.com/alexflint/go-arg"
)

// arguments structure
type Args struct {
	Nzblnk      string   `arg:"positional" help:"a qualified NZBLNK URI (nzblnk://?h=...)"`
	Header      string   `arg:"-s,--subject" help:"the header/subject to search for"`
	Title       string   `arg:"-t,--title" help:"the title/tag for the NZB file"`
	Password    string   `arg:"-p,--password" help:"the password to extract the download"`
	Groups      []string `arg:"-g,--group" help:"the group(s) to search in (several groups seperated with space)"`
	Date        string   `arg:"-d,--date" help:"the date the upload was posted to Usenet (either in the format DD.MM.YYYY or as a Unix timestamp)"`
	Category    string   `arg:"-c,--category" help:"the category to use for the target (if supportet by the target)"`
	UnixDate    int64    `arg:"-"` // will hold the parsed Unix timestamp
	IsTimestamp bool     `arg:"-"` // will indicate if exact timestamp was passed as date
	Config      string   `arg:"--config" help:"path to the config file"`
	Debug       bool     `arg:"--debug" help:"logs output to log file"`
	Register    bool     `arg:"--register" help:"register the NZBLNK protocol"`
	Serve       bool     `arg:"--serve" help:"run as web service accepting NZBLNK URIs via a web page and API"`
}

// version information
func (Args) Version() string {
	return " "
}

// additional description
func (Args) Epilogue() string {
	return "   Parameters that are passed as arguments have precedence over the parameters of the NZBLNK.\n\n   For more information visit github.com/Tensai75/nzb-monkey-go\n"
}

// global arguments variable
var args struct {
	Args
}

// parser variable
var argParser *parser.Parser

func parseArguments() {

	parserConfig := parser.Config{
		IgnoreEnv: true,
	}

	// parse flags
	argParser, _ = parser.NewParser(parserConfig, &args)
	if err := parser.Parse(&args); err != nil {
		if err.Error() == "help requested by user" {
			writeHelp(argParser)
			fmt.Println(args.Epilogue())
			exit(0)
		} else if err.Error() == "version requested by user" {
			fmt.Println(args.Version())
			exit(0)
		}
		writeUsage(argParser)
		Log.Error(err.Error())
		exit(1)
	}

}

func checkArguments() {

	if args.Register {
		fmt.Println()
		Log.Info("Registering the 'nzblnk' URL protocol ...")
		registerProtocol()
		exit(0)
	}

	if args.Header == "" && args.Nzblnk == "" {
		writeUsage(argParser)
		Log.Error("You must provide either --subject or a NZBLNK URI")
		exit(1)
	}

	// parse nzblink if provided
	isNzblnk := false
	if args.Nzblnk != "" {
		if link, err := parseNzblnk(args.Nzblnk); err == nil {
			if link.header == "" {
				writeUsage(argParser)
				Log.Error("Invalid NZBLNK URI: missing 'h' parameter")
				exit(1)
			}
			if args.Header == "" {
				args.Header = link.header
			}
			if link.title != "" && args.Title == "" {
				args.Title = link.title
			}
			if link.password != "" && args.Password == "" {
				args.Password = link.password
			}
			if args.Groups == nil {
				args.Groups = link.groups
			}
			if link.date != "" && args.Date == "" {
				isNzblnk = true
				args.Date = link.date
			}
		}
	}

	// make sure a header/subject was provided
	// (covers NZBLNK URIs that could not be parsed at all)
	if args.Header == "" {
		writeUsage(argParser)
		Log.Error("You must provide either --subject or a valid NZBLNK URI")
		exit(1)
	}

	// date argument needs to be parsed because it can have two formats
	if args.Date != "" {
		dateRegexDate := regexp.MustCompile(`^[0-3]\d\.[0-1]\d\.(?:19|20)\d\d$`)
		dateRegexTimestamp := regexp.MustCompile(`^[1-9]\d{9}$`)
		var parseError error
		if match := dateRegexDate.FindStringIndex(args.Date); match != nil {
			var date time.Time
			zone, _ := time.Now().Zone()
			if date, parseError = time.Parse("02.01.2006 MST", fmt.Sprintf("%s %s", args.Date, zone)); parseError == nil {
				args.UnixDate = date.Unix()
			}
		} else if match := dateRegexTimestamp.FindStringIndex(args.Date); match != nil {
			args.UnixDate, parseError = strconv.ParseInt(args.Date, 10, 64)
			args.IsTimestamp = true
		} else {
			parseError = fmt.Errorf("ERROR")
		}
		if parseError != nil || args.UnixDate == 0 {
			if isNzblnk {
				Log.Warn("Invalid NZBLNK URI: invalid input for parameter 'd'")
			} else {
				writeUsage(argParser)
				Log.Error("Invalid input for --date")
				exit(1)
			}
		}
	}

	// set title to header if empty
	if args.Title == "" {
		args.Title = args.Header
	}

	// replace a.b. prefix in groups
	for i, group := range args.Groups {
		if strings.HasPrefix(group, "a.b.") {
			args.Groups[i] = "alt.binaries." + strings.TrimPrefix(group, "a.b.")
		}
	}

	// unescape title
	args.Title = html.UnescapeString(args.Title)

}

// parsedNzblnk holds the extracted values of a NZBLNK URI.
type parsedNzblnk struct {
	header   string
	title    string
	password string
	groups   []string
	date     string
}

// parseNzblnk extracts the query parameters of a NZBLNK URI. It returns an
// error when the URI cannot be parsed at all; a missing 'h' parameter is not
// treated as an error here so callers can decide how strict to be.
func parseNzblnk(raw string) (*parsedNzblnk, error) {
	nzblnk, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	query, err := url.ParseQuery(nzblnk.RawQuery)
	if err != nil {
		return nil, err
	}
	link := &parsedNzblnk{
		header:   strings.TrimSpace(query.Get("h")),
		title:    strings.TrimSpace(query.Get("t")),
		password: strings.TrimSpace(query.Get("p")),
		date:     strings.TrimSpace(query.Get("d")),
	}
	for _, group := range query["g"] {
		if strings.Contains(group, ",") || strings.Contains(group, ";") || strings.Contains(group, " ") {
			divider := regexp.MustCompile(` *[,; ] *`)
			for _, splitGroup := range divider.Split(group, -1) {
				if splitGroup = strings.TrimSpace(splitGroup); splitGroup != "" {
					link.groups = append(link.groups, splitGroup)
				}
			}
		} else if group = strings.TrimSpace(group); group != "" {
			link.groups = append(link.groups, group)
		}
	}
	return link, nil
}

func writeUsage(parser *parser.Parser) {
	var buf bytes.Buffer
	parser.WriteUsage(&buf)
	scanner := bufio.NewScanner(&buf)
	for scanner.Scan() {
		fmt.Println("   " + scanner.Text())
	}

}

func writeHelp(parser *parser.Parser) {
	var buf bytes.Buffer
	parser.WriteHelp(&buf)
	scanner := bufio.NewScanner(&buf)
	for scanner.Scan() {
		fmt.Println("   " + scanner.Text())
	}

}
