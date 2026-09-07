package transport

import (
	"net/http"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/wekan/wekango/internal/clientip"
)

func init() { caddy.RegisterModule(ClientAddress{}) }

// ClientAddress runs before reverse_proxy modifies forwarded headers. The
// private HTTP listener trusts only this overwritten ingress value; changing
// Caddy's X-Forwarded-* trust rules is unnecessary.
type ClientAddress struct {
	ForwardedCount int64 `json:"forwarded_count"`
}

func (ClientAddress) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.wekan_client_address", New: func() caddy.Module { return new(ClientAddress) }}
}
func (h ClientAddress) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	caddyhttp.SetVar(r.Context(), "wekan_client_address", clientip.Resolve(r.Header, r.RemoteAddr, h.ForwardedCount))
	r.Header.Del(clientip.Header)
	return next.ServeHTTP(w, r)
}

var _ caddyhttp.MiddlewareHandler = (*ClientAddress)(nil)
