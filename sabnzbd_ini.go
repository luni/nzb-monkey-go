package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/ini.v1"
)

// importSabnzbdIni reads a sabnzbd.ini file and fills configuration fields
// that are not explicitly set in our own configuration file:
//   - [servers] -> enabled usenet server (host, port, ssl, username, password,
//     connections) into DIRECTSEARCH when no host is configured there
//   - [misc] -> api_key, web host/port and https flag into the SABNZBD target
//
// Only empty/unset fields are filled - explicitly configured values always win.
func importSabnzbdIni(c *Configuration, path string) error {

	// expand a leading ~/ to the user's home directory
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, rest)
		}
	}

	sabIni, err := ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: true}, path)
	if err != nil {
		return err
	}

	// import the enabled usenet server when no directsearch host is configured
	if c.Sabnzbd.UseServerCreds && c.Directsearch.Host == "" {
		if server := selectSabnzbdServer(sabIni); server != nil {
			c.Directsearch.Host = server.Key("host").String()
			if port := server.Key("port").MustInt(0); port > 0 {
				c.Directsearch.Port = port
			}
			c.Directsearch.SSL = server.Key("ssl").MustBool(false)
			if c.Directsearch.Username == "" {
				c.Directsearch.Username = server.Key("username").String()
			}
			if c.Directsearch.Password == "" {
				c.Directsearch.Password = server.Key("password").String()
			}
			if c.Directsearch.Connections <= 0 {
				if conns := server.Key("connections").MustInt(0); conns > 0 {
					c.Directsearch.Connections = conns
				}
			}
			Log.Info("Imported usenet server '%s' from sabnzbd.ini for directsearch", server.Key("host").String())
		}
	}

	// import SABnzbd web interface settings for the SABNZBD target
	misc := sabIni.Section("misc")
	if misc == nil {
		return nil
	}
	if c.Sabnzbd.Nzbkey == "" {
		if apikey := strings.TrimSpace(misc.Key("api_key").String()); apikey != "" {
			c.Sabnzbd.Nzbkey = apikey
		} else if nzbkey := strings.TrimSpace(misc.Key("nzb_key").String()); nzbkey != "" {
			// the nzb key only allows adding jobs - pause/status calls will fail
			c.Sabnzbd.Nzbkey = nzbkey
			Log.Warn("sabnzbd.ini has no api_key - imported nzb_key instead (add-jobs only)")
		}
	}
	if c.Sabnzbd.Host == "" {
		if host := strings.TrimSpace(misc.Key("host").String()); host != "" {
			c.Sabnzbd.Host = host
		}
	}
	if c.Sabnzbd.Port <= 0 {
		if port := misc.Key("port").MustInt(0); port > 0 {
			c.Sabnzbd.Port = port
		}
	}
	if !c.Sabnzbd.Ssl && misc.Key("enable_https").MustBool(false) {
		c.Sabnzbd.Ssl = true
	}
	return nil
}

// selectSabnzbdServer returns the enabled usenet server section with the
// lowest priority number (0 = highest priority in SABnzbd), or nil when no
// server is enabled. ini.v1 keeps SABnzbd's [[name]] subsections as literal
// "[name]" section names, so server sections are identified by their keys.
func selectSabnzbdServer(sabIni *ini.File) *ini.Section {
	var enabled []*ini.Section
	for _, section := range sabIni.Sections() {
		name := section.Name()
		if !(strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]")) {
			continue
		}
		if !section.HasKey("host") || !section.HasKey("priority") {
			continue
		}
		if !section.Key("enable").MustBool(false) {
			continue
		}
		enabled = append(enabled, section)
	}
	if len(enabled) == 0 {
		return nil
	}
	sort.Slice(enabled, func(i, j int) bool {
		return enabled[i].Key("priority").MustInt(0) < enabled[j].Key("priority").MustInt(0)
	})
	return enabled[0]
}

// sabnzbdBaseURL builds the API base URL from the SABNZBD target config.
func sabnzbdBaseURL() string {
	scheme := "http"
	if conf.Sabnzbd.Ssl {
		scheme = "https"
	}
	base := fmt.Sprintf("%s://%s", scheme, conf.Sabnzbd.Host)
	if conf.Sabnzbd.Port > 0 {
		base += fmt.Sprintf(":%d", conf.Sabnzbd.Port)
	}
	if bp := strings.Trim(conf.Sabnzbd.Basepath, " /"); bp != "" {
		base += "/" + bp
	}
	return base
}
