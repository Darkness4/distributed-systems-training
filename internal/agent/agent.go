package agent

import (
	"context"
	"crypto/tls"
	"distributed-systems/gen/log/v1/logv1connect"
	"distributed-systems/internal/auth"
	"distributed-systems/internal/discovery"
	internalhttp "distributed-systems/internal/http"
	"distributed-systems/internal/log"
	"distributed-systems/internal/log/replicator"
	"distributed-systems/internal/server"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type Config struct {
	ServerTLSConfig    *tls.Config
	PeerTLSConfig      *tls.Config
	DataDir            string
	BindAddress        string
	RPCPort            int
	NodeName           string
	StartJoinAddresses []string
	ACLModelFile       string
	ACLPolicyFile      string
}

// Agent is used for distributed logs using replication.
type Agent struct {
	Config

	log *log.Log

	server   *http.Server
	listener net.Listener

	membership *discovery.Membership
	replicator *replicator.Replicator

	shutdown   bool
	shutdownCh chan struct{}
	shutdownMu sync.Mutex
}

func (c Config) RPCAddress() (string, error) {
	host, _, err := net.SplitHostPort(c.BindAddress)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(c.RPCPort)), nil
}

func New(config Config) (*Agent, error) {
	a := &Agent{
		Config:     config,
		shutdownCh: make(chan struct{}),
	}
	for _, fn := range []func() error{
		a.setupLog,
		a.setupServer,
		a.setupMembership,
	} {
		if err := fn(); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *Agent) setupLog() error {
	var err error
	a.log, err = log.NewLog(a.DataDir, log.Config{})
	return err
}

func (a *Agent) setupServer() error {
	opts := []connect.HandlerOption{}
	interceptors := []connect.Interceptor{
		server.LoggingInterceptor(slog.With("component", "server")),
	}
	// ACL
	auth, err := auth.New(a.Config.ACLModelFile, a.Config.ACLPolicyFile)
	if err != nil {
		return err
	}
	interceptors = append(interceptors, auth.Interceptor())

	// Routes
	opts = append(opts, connect.WithInterceptors(interceptors...))
	r := http.NewServeMux()
	path, handler := server.NewLogAPIHandler(
		&server.Config{
			CommitLog: a.log,
		},
		opts...,
	)
	r.Handle(path, handler)

	// Listen
	rpcAddr, err := a.RPCAddress()
	if err != nil {
		return err
	}
	a.listener, err = tls.Listen("tcp", rpcAddr, a.ServerTLSConfig)
	if err != nil {
		return err
	}
	a.server = &http.Server{
		Handler: internalhttp.AuthMiddleware(h2c.NewHandler(r, &http2.Server{})),
	}
	go func() {
		if err := a.server.Serve(a.listener); err != nil {
			_ = a.Shutdown()
		}
	}()
	return err
}

func (a *Agent) setupMembership() error {
	rpcAddr, err := a.Config.RPCAddress()
	if err != nil {
		return err
	}
	httpClient := internalhttp.NewH2Client(internalhttp.WithTLSConfig(a.PeerTLSConfig))
	tls := a.PeerTLSConfig != nil
	var httpAddr string
	if tls {
		httpAddr = "https://" + rpcAddr
	} else {
		httpAddr = "http://" + rpcAddr

	}
	lc := logv1connect.NewLogAPIClient(httpClient, httpAddr, connect.WithGRPC())
	a.replicator = &replicator.Replicator{
		HTTP:        httpClient,
		TLS:         tls,
		LocalServer: lc,
	}
	a.membership, err = discovery.New(a.replicator, discovery.Config{
		NodeName:    a.Config.NodeName,
		BindAddress: a.Config.BindAddress,
		Tags: map[string]string{
			"rpc_addr": rpcAddr,
		},
		StartJoinAddresses: a.Config.StartJoinAddresses,
	})
	return err
}

func (a *Agent) Shutdown() error {
	a.shutdownMu.Lock()
	defer a.shutdownMu.Unlock()
	if a.shutdown {
		return nil
	}
	a.shutdown = true
	close(a.shutdownCh)

	for _, fn := range []func() error{
		a.membership.Leave,
		a.replicator.Close,
		func() error {
			_ = a.server.Shutdown(context.Background())
			_ = a.listener.Close()
			return nil
		},
		a.log.Close,
	} {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}
