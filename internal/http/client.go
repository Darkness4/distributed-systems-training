package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net"
	"net/http"
	"os"

	"golang.org/x/net/http2"
)

var DefaultClient = &http.Client{
	Transport: &http2.Transport{
		AllowHTTP: true,
		DialTLS: func(network, addr string, _ *tls.Config) (net.Conn, error) {
			return net.Dial(network, addr)
		},
	},
}

func NewTLSClient(crt, key, ca string) *http.Client {
	cfg := &tls.Config{}
	if err := SetupClientTLSConfig(crt, key, ca, cfg); err != nil {
		slog.Error("failed to setup TLS", "tls", err)
		cfg = nil
	}
	return &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {

				if cfg == nil {
					var d net.Dialer
					return d.DialContext(ctx, network, addr)
				}
				var d tls.Dialer
				d.Config = cfg
				return d.DialContext(ctx, network, addr)
			},
			TLSClientConfig: cfg,
		},
	}
}

func SetupClientTLSConfig(
	crt, key, ca string,
	cfg *tls.Config,
) error {
	if cfg == nil {
		return nil
	}
	if crt != "" && key != "" {
		certificate, err := tls.LoadX509KeyPair(crt, key)
		if err != nil {
			return err
		}
		cfg.Certificates = append(cfg.Certificates, certificate)
	}
	if ca != "" {
		cert, err := os.ReadFile(ca)
		if err != nil {
			return err
		}
		if cfg.RootCAs == nil {
			cfg.RootCAs = x509.NewCertPool()
		}
		cfg.RootCAs.AppendCertsFromPEM(cert)
	}
	return nil
}

func SetupServerTLSConfig(
	crt, key, ca, serverName string,
	cfg *tls.Config,
) error {
	if cfg == nil {
		return nil
	}
	if crt != "" && key != "" {
		certificate, err := tls.LoadX509KeyPair(crt, key)
		if err != nil {
			return err
		}
		cfg.Certificates = append(cfg.Certificates, certificate)
	}
	if ca != "" {
		cert, err := os.ReadFile(ca)
		if err != nil {
			return err
		}
		if cfg.ClientCAs == nil {
			cfg.ClientCAs = x509.NewCertPool()
		}
		cfg.ClientCAs.AppendCertsFromPEM(cert)
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
		cfg.ServerName = serverName
	}
	return nil
}
