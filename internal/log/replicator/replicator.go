package replicator

import (
	"context"
	logv1 "distributed-systems/gen/log/v1"
	"distributed-systems/gen/log/v1/logv1connect"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"connectrpc.com/connect"
)

type Replicator struct {
	LocalServer logv1connect.LogAPIClient
	HTTP        *http.Client
	TLS         bool

	logger *slog.Logger

	mu sync.Mutex
	// servers contains leave channels for each server.
	servers map[string]chan struct{}
	closed  bool
	close   chan struct{}
}

func (r *Replicator) Join(name, addr string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()

	if r.closed {
		return nil
	}

	if _, ok := r.servers[name]; ok {
		// already replicating
		return nil
	}
	r.servers[name] = make(chan struct{})
	go r.replicate(addr, r.servers[name])
	return nil
}

func (r *Replicator) replicate(addr string, leave chan struct{}) {
	if r.TLS {
		addr = "https://" + addr
	} else {
		addr = "http://" + addr
	}
	// Consume from remote and produce to local.
	client := logv1connect.NewLogAPIClient(r.HTTP, addr, connect.WithGRPC())
	ctx := context.Background()
	stream, err := client.ConsumeStream(ctx, &connect.Request[logv1.ConsumeStreamRequest]{
		Msg: &logv1.ConsumeStreamRequest{
			Offset: 0,
		},
	})
	if err != nil {
		r.logger.Error("failed to consume stream", "addr", addr, "err", err)
		return
	}

	records := make(chan *logv1.Record, 16)
	go func() {
		for stream.Receive() {
			fmt.Println("produce", stream.Msg())
			records <- stream.Msg().Record
		}
		if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) &&
			!errors.Is(err, context.Canceled) {
			r.logger.Error("failed to receive record", "addr", addr, "err", err)
		}
	}()

	for {
		select {
		case <-r.close:
			return
		case <-leave:
			return
		case record := <-records:
			_, err = r.LocalServer.Produce(ctx,
				&connect.Request[logv1.ProduceRequest]{
					Msg: &logv1.ProduceRequest{
						Record: record,
					},
				},
			)
			if err != nil {
				r.logger.Error("failed to produce record", "addr", addr, "err", err)
				return
			}
		}
	}
}

func (r *Replicator) Leave(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()
	if _, ok := r.servers[name]; !ok {
		return nil
	}
	close(r.servers[name])
	delete(r.servers, name)
	return nil
}

func (r *Replicator) init() {
	if r.logger == nil {
		r.logger = slog.With("component", "replicator")
	}
	if r.servers == nil {
		r.servers = make(map[string]chan struct{})
	}
	if r.close == nil {
		r.close = make(chan struct{})
	}
}

func (r *Replicator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.init()

	if r.closed {
		return nil
	}
	r.closed = true
	close(r.close)
	return nil
}
