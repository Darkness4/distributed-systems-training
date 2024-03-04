package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"distributed-systems/internal/auth"
	"distributed-systems/internal/discovery"
	internalhttp "distributed-systems/internal/http"
	"distributed-systems/internal/log"
	"distributed-systems/internal/log/distributed"
	"distributed-systems/internal/server"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/hashicorp/raft"
	"github.com/soheilhy/cmux"
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
	Bootstrap          bool
}

// Agent is used for distributed logs using replication.
type Agent struct {
	Config

	mux cmux.CMux
	log *distributed.Log

	server *http.Server

	membership *discovery.Membership

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
		a.setupMux,
		a.setupLog,
		a.setupServer,
		a.setupMembership,
	} {
		if err := fn(); err != nil {
			return nil, err
		}
	}
	go a.serve()
	return a, nil
}

// TODO: replace cmux with out own mux
func (a *Agent) setupMux() error {
	rpcAddr := fmt.Sprintf(
		":%d",
		a.Config.RPCPort,
	)
	ln, err := net.Listen("tcp", rpcAddr)
	if err != nil {
		return err
	}
	a.mux = cmux.New(ln)
	return nil
}

func (a *Agent) setupLog() error {
	raftLn := a.mux.Match(func(r io.Reader) bool {
		b := make([]byte, 1)
		if _, err := r.Read(b); err != nil {
			return false
		}
		return bytes.Equal(b, []byte{byte(distributed.RaftRPC)})
	})
	cfg := log.Config{
		Raft: log.Raft{
			StreamLayer: distributed.NewStreamLayer(
				raftLn,
				a.ServerTLSConfig,
				a.PeerTLSConfig,
			),
			Config: raft.Config{
				LocalID: raft.ServerID(a.Config.NodeName),
			},
			Bootstrap: a.Config.Bootstrap,
		},
	}
	var err error
	a.log, err = distributed.NewLog(a.DataDir, cfg)
	if err != nil {
		return err
	}
	if a.Config.Bootstrap {
		err = a.log.WaitForLeader(3 * time.Second)
	}
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
	a.server = &http.Server{
		Handler: internalhttp.AuthMiddleware(h2c.NewHandler(r, &http2.Server{})),
	}
	grpcLn := a.mux.Match(cmux.Any())
	if a.ServerTLSConfig != nil {
		grpcLn = tls.NewListener(grpcLn, a.ServerTLSConfig)
	}
	go func() {
		if err := a.server.Serve(grpcLn); err != nil {
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
	a.membership, err = discovery.New(a.log, discovery.Config{
		NodeName:    a.Config.NodeName,
		BindAddress: a.Config.BindAddress,
		Tags: map[string]string{
			"rpc_addr": rpcAddr,
		},
		StartJoinAddresses: a.Config.StartJoinAddresses,
	})
	return err
}

func (a *Agent) serve() error {
	if err := a.mux.Serve(); err != nil {
		_ = a.Shutdown()
		return err
	}
	return nil
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
		func() error {
			_ = a.server.Shutdown(context.Background())
			a.mux.Close()
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
