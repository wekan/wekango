package transport

import (
	"encoding/json"
	"github.com/wekan/wekango/internal/config"
	"strings"
	"testing"
)

func TestExistingTLSProxyRemainsHTTP(t *testing.T) {
	c, e := config.Load(func(k string) string {
		if k == "ROOT_URL" {
			return "https://example.com/wekan"
		}
		return ""
	}, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	raw, e := Configuration(c, "127.0.0.1:12345")
	if e != nil {
		t.Fatal(e)
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		t.Fatal("bad JSON")
	}
	for _, s := range []string{`"disabled":true`, `"persist":false`, `"automatic_https":{"disable":true}`, `127.0.0.1:12345`} {
		if !strings.Contains(string(raw), s) {
			t.Errorf("missing %s", s)
		}
	}
}
func TestAutomaticTLSOptIn(t *testing.T) {
	c, e := config.Load(func(k string) string {
		switch k {
		case "ROOT_URL":
			return "https://example.com"
		case "CADDY_AUTO_HTTPS":
			return "true"
		}
		return ""
	}, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := Configuration(c, "127.0.0.1:12345")
	if !strings.Contains(string(raw), `"host":["example.com"]`) || strings.Contains(string(raw), `"automatic_https":{"disable":true}`) {
		t.Fatal(string(raw))
	}
}

func TestCaddyModulesProvision(t *testing.T) {
	c := config.Config{Port: "0", BindIP: "127.0.0.1", RootURL: "http://localhost", WritablePath: t.TempDir()}
	if err := Start(c, "127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := Stop(); err != nil {
			t.Error(err)
		}
	})
}
