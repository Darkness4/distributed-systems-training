package main

import (
	"context"
	"crypto/tls"
	"distributed-systems/internal/auth"
	internalhttp "distributed-systems/internal/http"
	"distributed-systems/internal/otel"
	"distributed-systems/internal/server"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"github.com/joho/godotenv"
	"github.com/urfave/cli/v2"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

var (
	version       string
	crt           string
	key           string
	ca            string
	serverName    string
	listenAddress string
	aclModelFile  string
	aclPolicyFile string
)

var app = &cli.App{
	Name:    "distributed-systems",
	Version: version,
	Usage:   "Example of a distributed system",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:        "listen-address",
			Usage:       "Address to listen on.",
			EnvVars:     []string{"LISTEN_ADDRESS"},
			Value:       ":8080",
			Destination: &listenAddress,
		},
		&cli.StringFlag{
			Name:        "crt",
			Usage:       "Path to the certificate.",
			EnvVars:     []string{"CERT"},
			Destination: &crt,
		},
		&cli.StringFlag{
			Name:        "key",
			Usage:       "Path to the key.",
			EnvVars:     []string{"KEY"},
			Destination: &key,
		},
		&cli.StringFlag{
			Name:        "ca",
			Usage:       "Path to the certificate authority.",
			EnvVars:     []string{"CA"},
			Destination: &ca,
		},
		&cli.StringFlag{
			Name:        "server-name",
			Usage:       "Server domain name (for mutual tls)",
			EnvVars:     []string{"SERVER_NAME"},
			Destination: &serverName,
		},
		&cli.StringFlag{
			Name:        "acl-model-file",
			Usage:       "Path to the ACL model file.",
			EnvVars:     []string{"ACL_MODEL_FILE"},
			Destination: &aclModelFile,
		},
		&cli.StringFlag{
			Name:        "acl-policy-file",
			Usage:       "Path to the ACL policy file.",
			EnvVars:     []string{"ACL_POLICY_FILE"},
			Destination: &aclPolicyFile,
		},
	},
	Action: func(c *cli.Context) error {
		ctx := c.Context
		opts := []connect.HandlerOption{}
		interceptors := []connect.Interceptor{
			server.LoggingInterceptor(slog.With("component", "server")),
		}

		// Handle SIGINT (CTRL+C) gracefully.
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
		defer stop()

		// ACL
		if aclModelFile != "" && aclPolicyFile != "" {
			auth, err := auth.New(aclModelFile, aclPolicyFile)
			if err != nil {
				return err
			}
			interceptors = append(interceptors, auth.Interceptor())
		}

		// OTEL
		traceProvider, meterProvider, prop, otelShutdown, err := otel.SetupOTelSDK(ctx, nil, nil)
		if err != nil {
			return err
		}
		defer func() {
			_ = otelShutdown(ctx)
		}()
		otel, err := otelconnect.NewInterceptor(
			otelconnect.WithTracerProvider(traceProvider),
			otelconnect.WithMeterProvider(meterProvider),
			otelconnect.WithPropagator(prop),
		)
		if err != nil {
			return err
		}
		interceptors = append(interceptors, otel)

		// Routes
		opts = append(opts, connect.WithInterceptors(interceptors...))
		r := http.NewServeMux()
		path, handler := server.NewLogAPIHandler(
			&server.Config{},
			opts...,
		)
		r.Handle(path, handler)

		// TLS
		slog.Info("Server started", "address", listenAddress, "tls", crt != "")
		tlsConfig := &tls.Config{}
		if err := internalhttp.SetupServerTLSConfig(crt, key, ca, serverName, tlsConfig); err != nil {
			slog.Error("error setting up tls", "error", err)
			tlsConfig = nil
		}

		// Server
		l, err := tls.Listen("tcp", ":0", tlsConfig)
		if err != nil {
			return err
		}
		srv := &http.Server{
			BaseContext: func(_ net.Listener) context.Context { return ctx },
			Handler:     internalhttp.AuthMiddleware(h2c.NewHandler(r, &http2.Server{})),
		}
		defer func() {
			_ = srv.Shutdown(ctx)
			_ = l.Close()
			slog.Info("Server stopped")
		}()
		srvErr := make(chan error, 1)
		go func() {
			srvErr <- srv.Serve(l)
		}()

		// Wait for interruption.
		select {
		case err = <-srvErr:
			// Error when starting HTTP server.
			return err
		case <-ctx.Done():
			// Wait for first CTRL+C.
			// Stop receiving signal notifications as soon as possible.
			stop()
		}
		return nil
	},
}

func main() {
	_ = godotenv.Load(".env.local")
	_ = godotenv.Load(".env")
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
