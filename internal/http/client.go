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

type Option func(*Options)

type Options struct {
	tlsConfig *tls.Config
}

func WithTLSConfig(cfg *tls.Config) Option {
	return func(o *Options) {
		o.tlsConfig = cfg
	}
}

func WithTLS(crt, key, ca, serverName string) Option {
	var cfg *tls.Config
	var err error
	cfg, err = SetupClientTLSConfig(crt, key, ca, serverName)
	if err != nil {
		slog.Error("failed to setup TLS", "tls", err)
		cfg = nil
	}
	return WithTLSConfig(cfg)
}

func NewH2Client(opts ...Option) *http.Client {
	var options Options
	for _, o := range opts {
		o(&options)
	}
	return &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
				var d net.Dialer
				conn, err := d.DialContext(ctx, network, addr)
				if cfg != nil {
					return tls.Client(conn, cfg), err
				}
				return conn, err
			},
			TLSClientConfig: options.tlsConfig,
		},
	}
}

func SetupClientTLSConfig(
	crt, key, ca, serverName string,
) (*tls.Config, error) {
	cfg := &tls.Config{}
	if crt != "" && key != "" {
		certificate, err := tls.LoadX509KeyPair(crt, key)
		if err != nil {
			return nil, err
		}
		cfg.Certificates = append(cfg.Certificates, certificate)
	}
	if ca != "" {
		cert, err := os.ReadFile(ca)
		if err != nil {
			return nil, err
		}
		if cfg.RootCAs == nil {
			cfg.RootCAs = x509.NewCertPool()
		}
		cfg.RootCAs.AppendCertsFromPEM(cert)
		cfg.ServerName = serverName
	}
	return cfg, nil
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
