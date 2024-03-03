package main

import (
	"crypto/tls"
	"distributed-systems/internal/auth"
	internalhttp "distributed-systems/internal/http"
	"distributed-systems/internal/server"
	"log"
	"log/slog"
	"net/http"
	"os"

	"connectrpc.com/connect"
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
	Action: func(_ *cli.Context) error {
		opts := []connect.HandlerOption{}
		if aclModelFile != "" && aclPolicyFile != "" {
			auth := auth.New(aclModelFile, aclPolicyFile)
			opts = append(opts, connect.WithInterceptors(auth.Interceptor()))
		}

		r := http.NewServeMux()
		path, handler := server.NewLogAPIHandler(
			&server.Config{},
			opts...,
		)
		r.Handle(path, handler)

		slog.Info("Server started", "address", listenAddress, "tls", crt != "")
		tlsConfig := &tls.Config{}
		if err := internalhttp.SetupServerTLSConfig(crt, key, ca, serverName, tlsConfig); err != nil {
			slog.Error("error setting up tls", "error", err)
			tlsConfig = nil
		}

		l, err := tls.Listen("tcp", ":0", tlsConfig)
		if err != nil {
			return err
		}
		return http.Serve(l, internalhttp.AuthMiddleware(h2c.NewHandler(r, &http2.Server{})))
	},
}

func main() {
	_ = godotenv.Load(".env.local")
	_ = godotenv.Load(".env")
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
