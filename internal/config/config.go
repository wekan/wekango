// Package config preserves the standalone WeKan bundle's configuration layout.
package config

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port, BindIP, RootURL, Prefix                                                     string
	MongoURL, Database                                                                string
	WritablePath, FilesPath, AttachmentsPath, AvatarsPath, SQLiteDirectory, SQLiteURL string
	WithAPI, AutoHTTPS                                                                bool
	LoginExpiration                                                                   time.Duration
}

// Load uses an injected environment and executable directory so launcher defaults
// are deterministic and testable. Explicit WRITABLE_PATH stays relative to cwd,
// as it does in Meteor; only the standalone default is beside the executable.
func Load(getenv func(string) string, executableDir string) (Config, error) {
	c := Config{Port: getenv("PORT"), BindIP: getenv("BIND_IP"), RootURL: getenv("ROOT_URL"), MongoURL: getenv("MONGO_URL"), Database: "wekan", WritablePath: getenv("WRITABLE_PATH"), SQLiteDirectory: getenv("FERRETDB_SQLITE_DIR"), SQLiteURL: getenv("FERRETDB_SQLITE_URL"), LoginExpiration: 90 * 24 * time.Hour}
	c.AutoHTTPS = getenv("CADDY_AUTO_HTTPS") == "true"
	if c.AutoHTTPS && c.Port == "" {
		c.Port = "443"
	}
	if c.Port == "" {
		c.Port = "8080"
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return c, fmt.Errorf("PORT must be 1..65535")
	}
	if c.BindIP != "" && net.ParseIP(c.BindIP) == nil {
		return c, fmt.Errorf("BIND_IP must be an IP address")
	}
	if c.RootURL == "" {
		c.RootURL = "http://localhost:" + c.Port
	}
	u, err := url.Parse(c.RootURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return c, fmt.Errorf("ROOT_URL must be an HTTP(S) origin with an optional path prefix")
	}
	if strings.Contains(u.Path, "..") || u.RawPath != "" {
		return c, fmt.Errorf("ROOT_URL path must be canonical")
	}
	if c.AutoHTTPS && u.Scheme != "https" {
		return c, fmt.Errorf("CADDY_AUTO_HTTPS requires an HTTPS ROOT_URL")
	}
	if forwarded := getenv("HTTP_FORWARDED_COUNT"); forwarded != "" && forwarded != "0" {
		return c, fmt.Errorf("HTTP_FORWARDED_COUNT is not supported in this compatibility preview; trusted external proxy configuration is pending")
	}
	c.Prefix = strings.TrimRight(u.Path, "/")
	if c.WritablePath == "" {
		c.WritablePath = filepath.Join(executableDir, "data")
	}
	c.FilesPath = c.WritablePath
	// Match the app's /files and Windows \\files suffix rules, without nesting files.
	if !strings.HasSuffix(strings.TrimRight(c.FilesPath, "/\\"), "/files") && !strings.HasSuffix(strings.TrimRight(c.FilesPath, "/\\"), "\\files") {
		c.FilesPath = filepath.Join(c.FilesPath, "files")
	}
	c.FilesPath = filepath.Clean(c.FilesPath)
	c.AttachmentsPath = filepath.Join(c.FilesPath, "attachments")
	c.AvatarsPath = filepath.Join(c.FilesPath, "avatars")
	if c.SQLiteDirectory == "" {
		c.SQLiteDirectory = filepath.Join(c.FilesPath, "db")
	}
	api := getenv("WITH_API")
	c.WithAPI = api == "" || api == "true" // standalone launcher default
	if days := getenv("ACCOUNTS_COMMON_LOGIN_EXPIRATION_IN_DAYS"); days != "" {
		n, e := strconv.Atoi(days)
		if e != nil || n < 1 || n > 36500 {
			return c, fmt.Errorf("invalid ACCOUNTS_COMMON_LOGIN_EXPIRATION_IN_DAYS")
		}
		c.LoginExpiration = time.Duration(n) * 24 * time.Hour
	}
	if c.MongoURL != "" {
		m, e := url.Parse(c.MongoURL)
		if e != nil || (m.Scheme != "mongodb" && m.Scheme != "mongodb+srv") || m.Host == "" {
			return c, fmt.Errorf("MONGO_URL must be a MongoDB URI")
		}
		if m.Path != "" && m.Path != "/" {
			c.Database = strings.TrimPrefix(m.Path, "/")
		}
		if strings.Contains(c.Database, "/") {
			return c, fmt.Errorf("MONGO_URL must name one database")
		}
	}
	return c, nil
}
