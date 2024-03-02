package http

import (
	"crypto/tls"
	"net"
	"net/http"

	"golang.org/x/net/http2"
)

var DefaultClient = &http.Client{
	Transport: &http2.Transport{
		AllowHTTP: true,
		DialTLS: func(network, addr string, _ *tls.Config) (net.Conn, error) {
			// If you're also using this client for non-h2c traffic, you may want
			// to delegate to tls.Dial if the network isn't TCP or the addr isn't
			// in an allowlist.
			return net.Dial(network, addr)
		},
		// Don't forget timeouts!
	},
}
