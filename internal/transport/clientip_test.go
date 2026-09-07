package transport

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wekan/wekango/internal/clientip"
	"github.com/wekan/wekango/internal/config"
)

// TestClientAddressThroughCaddy exercises the registered middleware at the real
// external listener, before Caddy rewrites X-Forwarded-For for its private hop.
// These tests must stay sequential: Caddy configuration is process-global.
func TestClientAddressThroughCaddy(t *testing.T) {
	const externalPeer = "127.0.0.2"
	type forwardingCase struct {
		name string
		xff  []string
		want string
	}
	for _, setting := range []struct {
		name  string
		count int64
		cases []forwardingCase
	}{
		{"zero", 0, []forwardingCase{
			{"forged-forwarding", []string{"198.51.100.99, 203.0.113.21"}, externalPeer},
			{"duplicate-forwarding", []string{"198.51.100.99", "203.0.113.21"}, externalPeer},
			{"no-forwarding", nil, externalPeer},
		}},
		{"one", 1, []forwardingCase{
			{"one-proxy", []string{"203.0.113.21"}, "203.0.113.21"},
			{"left-spoof", []string{"198.51.100.99, 203.0.113.21"}, "203.0.113.21"},
			{"empty-entries", []string{" , 198.51.100.99,,203.0.113.21 , "}, "203.0.113.21"},
			{"duplicate-forwarding", []string{"198.51.100.99", "203.0.113.21"}, "203.0.113.21"},
			{"empty-chain", []string{" , , "}, externalPeer},
			{"missing-chain", nil, externalPeer},
		}},
		{"two", 2, []forwardingCase{
			{"two-proxies", []string{"203.0.113.21, 192.0.2.44"}, "203.0.113.21"},
			{"left-spoof", []string{"198.51.100.99, 203.0.113.21, 192.0.2.44"}, "203.0.113.21"},
			{"empty-entries", []string{" , 198.51.100.99,,203.0.113.21, ,192.0.2.44,"}, "203.0.113.21"},
			{"duplicate-forwarding", []string{"198.51.100.99, 203.0.113.21", "192.0.2.44"}, "203.0.113.21"},
			{"insufficient-chain", []string{"203.0.113.21"}, externalPeer},
			{"empty-chain", []string{" , , "}, externalPeer},
			{"missing-chain", nil, externalPeer},
		}},
	} {
		t.Run(setting.name, func(t *testing.T) {
			type observed struct {
				Addresses                  []string `json:"addresses"`
				Remote, Method, Body, Path string
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(observed{Addresses: r.Header.Values(clientip.Header), Remote: r.RemoteAddr, Method: r.Method, Body: string(body), Path: r.URL.RequestURI()})
			}))
			t.Cleanup(upstream.Close)
			upstreamURL, err := url.Parse(upstream.URL)
			if err != nil {
				t.Fatal(err)
			}
			// Reserve an available external port before passing the address through the
			// production Configuration/Start path (port 0 is not externally discoverable).
			reservation, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := reservation.Addr().String()
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				reservation.Close()
				t.Fatal(err)
			}
			if err = reservation.Close(); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{BindIP: "127.0.0.1", Port: port, RootURL: "http://" + address, WritablePath: t.TempDir(), HTTPForwardedCount: setting.count}
			if err = Start(cfg, upstreamURL.Host); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := Stop(); err != nil {
					t.Error(err)
				}
			})
			// Binding the caller to another loopback address proves a fallback uses the
			// external peer, not the 127.0.0.1 private Caddy -> httptest connection.
			dialer := &net.Dialer{Timeout: 5 * time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP(externalPeer)}}
			transport := &http.Transport{DialContext: dialer.DialContext}
			t.Cleanup(transport.CloseIdleConnections)
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			for _, tc := range setting.cases {
				for _, method := range []string{http.MethodGet, http.MethodPost} {
					for _, connection := range []string{"", clientip.Header + ", X-Forwarded-For"} {
						t.Run(tc.name+"/"+method+"/Connection="+connection, func(t *testing.T) {
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							body := ""
							if method == http.MethodPost {
								body = "payload through address middleware"
							}
							req, err := http.NewRequestWithContext(ctx, method, "http://"+address+"/nested/resource?value=kept", strings.NewReader(body))
							if err != nil {
								t.Fatal(err)
							}
							// Both attacker-controlled copies must be replaced, even when the selected
							// forwarding value is itself absent and the module falls back to socket IP.
							if connection != "" {
								req.Header.Set("Connection", connection)
							}
							req.Header.Add(clientip.Header, "forged-first")
							req.Header.Add(clientip.Header, "forged-second")
							for _, value := range tc.xff {
								req.Header.Add("X-Forwarded-For", value)
							}
							res, err := client.Do(req)
							if err != nil {
								t.Fatal(err)
							}
							defer res.Body.Close()
							if res.StatusCode != 200 {
								response, _ := io.ReadAll(res.Body)
								t.Fatalf("HTTP %d: %s", res.StatusCode, response)
							}
							var got observed
							if err = json.NewDecoder(res.Body).Decode(&got); err != nil {
								t.Fatal(err)
							}
							if len(got.Addresses) != 1 || got.Addresses[0] != tc.want {
								t.Fatalf("count %d forwarding %#v: upstream addresses %#v, want only %q", setting.count, tc.xff, got.Addresses, tc.want)
							}
							privatePeer, _, err := net.SplitHostPort(got.Remote)
							if err != nil {
								t.Fatal(err)
							}
							if privatePeer == externalPeer {
								t.Fatalf("fixture did not distinguish external caller from private proxy peer: %s", got.Remote)
							}
							if tc.want == externalPeer && got.Addresses[0] == privatePeer {
								t.Fatalf("socket fallback used the private proxy peer %s", privatePeer)
							}
							if got.Method != method || got.Body != body || got.Path != "/nested/resource?value=kept" {
								t.Fatalf("middleware changed request: %#v", got)
							}
						})
					}
				}
			}
		})
	}
}
