// Package transport owns Caddy inside the same process as the application.
package transport

import (
	"encoding/json"
	"net"
	"net/url"
	"path/filepath"

	"github.com/caddyserver/caddy/v2"
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp"
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	_ "github.com/caddyserver/caddy/v2/modules/filestorage"
	"github.com/wekan/wekango/internal/config"
)

// Configuration keeps Meteor's plain HTTP PORT default, including when ROOT_URL
// is HTTPS behind an existing reverse proxy. Automatic TLS is a separate opt-in.
// The private upstream is never taken from user input or exposed outside loopback.
func Configuration(c config.Config, upstream string) ([]byte, error) {
	server := map[string]any{
		"listen":              []string{net.JoinHostPort(c.BindIP, c.Port)},
		"read_header_timeout": "10s", "idle_timeout": "2m", "max_header_bytes": 1 << 20,
	}
	route := map[string]any{"handle": []any{map[string]any{"handler": "wekan_client_address", "forwarded_count": c.HTTPForwardedCount}, map[string]any{"handler": "reverse_proxy", "headers": map[string]any{"request": map[string]any{"set": map[string]any{"X-Wekan-Client-IP": []string{"{http.vars.wekan_client_address}"}}}}, "upstreams": []any{map[string]any{"dial": upstream}}}}, "terminal": true}
	if c.AutoHTTPS {
		u, _ := url.Parse(c.RootURL)
		route["match"] = []any{map[string]any{"host": []string{u.Hostname()}}}
	} else {
		server["automatic_https"] = map[string]any{"disable": true}
	}
	server["routes"] = []any{route}
	storage, err := filepath.Abs(filepath.Join(c.WritablePath, "caddy"))
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"admin":   map[string]any{"disabled": true, "config": map[string]any{"persist": false}},
		"storage": map[string]any{"module": "file_system", "root": storage},
		"apps":    map[string]any{"http": map[string]any{"servers": map[string]any{"wekan": server}}},
	})
}
func Start(c config.Config, upstream string) error {
	data, err := Configuration(c, upstream)
	if err != nil {
		return err
	}
	return caddy.Load(data, true)
}
func Stop() error { return caddy.Stop() }
