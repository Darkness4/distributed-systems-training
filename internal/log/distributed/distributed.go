package distributed

import (
	"bytes"
	logv1 "distributed-systems/gen/log/v1"
	"distributed-systems/internal/log"
	"distributed-systems/internal/raftpebble"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/hashicorp/raft"
	"google.golang.org/protobuf/proto"
)

type Log struct {
	config log.Config
	log    *log.Log
	raft   *raft.Raft
}

func NewLog(dataDir string, config log.Config) (
	*Log,
	error,
) {
	l := &Log{
		config: config,
	}
	if err := l.setupLog(dataDir); err != nil {
		return nil, err
	}
	if err := l.setupRaft(dataDir); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Log) setupLog(dataDir string) error {
	logDir := filepath.Join(dataDir, "log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}
	var err error
	l.log, err = log.NewLog(logDir, l.config)
	return err
}

func (l *Log) setupRaft(dataDir string) error {
	fsm := &fsm{log: l.log}

	logDir := filepath.Join(dataDir, "raft", "log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}
	logConfig := l.config
	logConfig.Segment.InitialOffset = 1
	ldb, err := newLogStore(logDir, logConfig)
	if err != nil {
		return err
	}

	sdb, err := raftpebble.New(
		raftpebble.WithDbDirPath(filepath.Join(dataDir, "raft", "stable")),
		raftpebble.WithLogger(pebble.DefaultLogger),
	)
	if err != nil {
		return err
	}

	retain := 1
	fss, err := raft.NewFileSnapshotStore(
		filepath.Join(dataDir, "raft"),
		retain,
		os.Stderr,
	)
	if err != nil {
		return err
	}

	maxPool := 5
	timeout := 10 * time.Second
	transport := raft.NewNetworkTransport(
		l.config.Raft.StreamLayer,
		maxPool,
		timeout,
		os.Stderr,
	)

	config := raft.DefaultConfig()
	config.LocalID = l.config.Raft.LocalID
	if l.config.Raft.HeartbeatTimeout != 0 {
		config.HeartbeatTimeout = l.config.Raft.HeartbeatTimeout
	}
	if l.config.Raft.ElectionTimeout != 0 {
		config.ElectionTimeout = l.config.Raft.ElectionTimeout
	}
	if l.config.Raft.LeaderLeaseTimeout != 0 {
		config.LeaderLeaseTimeout = l.config.Raft.LeaderLeaseTimeout
	}
	if l.config.Raft.CommitTimeout != 0 {
		config.CommitTimeout = l.config.Raft.CommitTimeout
	}

	l.raft, err = raft.NewRaft(
		config,
		fsm,
		ldb,
		sdb,
		fss,
		transport,
	)
	if err != nil {
		return err
	}

	// Check if there is an existing state, if not bootstrap.
	hasState, err := raft.HasExistingState(
		ldb,
		sdb,
		fss,
	)
	if err != nil {
		return err
	}
	if l.config.Raft.Bootstrap && !hasState {
		slog.Info(
			"bootstrapping new raft node",
			"id",
			config.LocalID,
			"addr",
			transport.LocalAddr(),
		)
		config := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      config.LocalID,
					Address: transport.LocalAddr(),
				},
			},
		}
		err = l.raft.BootstrapCluster(config).Error()
	}
	return err
}

func (l *Log) Append(record *logv1.Record) (uint64, error) {
	res, err := l.apply(AppendRequestType, &logv1.ProduceRequest{
		Record: record,
	})
	if err != nil {
		return 0, err
	}
	return res.(*logv1.ProduceResponse).Offset, nil
}

func (l *Log) apply(reqType RequestType, req proto.Message) (
	interface{},
	error,
) {
	var buf bytes.Buffer
	_, err := buf.Write([]byte{byte(reqType)})
	if err != nil {
		return nil, err
	}
	b, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}
	_, err = buf.Write(b)
	if err != nil {
		return nil, err
	}
	timeout := 10 * time.Second
	future := l.raft.Apply(buf.Bytes(), timeout)
	if future.Error() != nil {
		return nil, future.Error()
	}
	res := future.Response()
	if err, ok := res.(error); ok {
		return nil, err
	}
	return res, nil
}

func (l *Log) Read(offset uint64) (*logv1.Record, error) {
	return l.log.Read(offset)
}

func (l *Log) Join(id, addr string) error {
	slog.Info("received join request", "id", id, "addr", addr)

	configFuture := l.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		slog.Error("failed to get raft configuration", "error", err)
		return err
	}
	// Check if the server has already joined
	for _, srv := range configFuture.Configuration().Servers {
		// If a node already exists with either the joining node's ID or address,
		// that node may need to be removed from the config first.
		if srv.ID == raft.ServerID(id) || srv.Address == raft.ServerAddress(addr) {
			// However if *both* the ID and the address are the same, then nothing -- not even
			// a join operation -- is needed.
			if srv.Address == raft.ServerAddress(addr) && srv.ID == raft.ServerID(id) {
				slog.Info(
					"node already member of cluster, ignoring join request",
					"id",
					id,
					"addr",
					addr,
				)
				return nil
			}

			future := l.raft.RemoveServer(raft.ServerID(id), 0, 0)
			if err := future.Error(); err != nil {
				return fmt.Errorf("error removing existing node %s at %s: %s", id, addr, err)
			}
		}
	}

	// Add the new server
	addFuture := l.raft.AddVoter(raft.ServerID(id), raft.ServerAddress(addr), 0, 0)
	if err := addFuture.Error(); err != nil {
		return err
	}
	return nil
}

func (l *Log) Leave(id string) error {
	removeFuture := l.raft.RemoveServer(raft.ServerID(id), 0, 0)
	return removeFuture.Error()
}

func (l *Log) WaitForLeader(timeout time.Duration) error {
	timeoutCh := time.After(timeout)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timeoutCh:
			return errors.New("timed out waiting for leader")
		case <-ticker.C:
			addr, _ := l.raft.LeaderWithID()
			if addr != "" {
				return nil
			}
		}
	}
}

func (l *Log) Close() error {
	if err := l.raft.Shutdown().Error(); err != nil {
		return err
	}
	return l.log.Close()
}
