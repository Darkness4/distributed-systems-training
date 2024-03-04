package distributed

import (
	logv1 "distributed-systems/gen/log/v1"
	"distributed-systems/internal/log"

	"github.com/hashicorp/raft"
)

var _ raft.LogStore = (*logStore)(nil)

type logStore struct {
	*log.Log
}

func newLogStore(dir string, c log.Config) (*logStore, error) {
	log, err := log.NewLog(dir, c)
	if err != nil {
		return nil, err
	}
	return &logStore{log}, nil
}

// DeleteRange implements raft.LogStore.
func (l *logStore) DeleteRange(_ uint64, max uint64) error {
	return l.Truncate(max)
}

// FirstIndex implements raft.LogStore.
func (l *logStore) FirstIndex() (uint64, error) {
	return l.LowestOffset()
}

// GetLog implements raft.LogStore.
func (l *logStore) GetLog(index uint64, log *raft.Log) error {
	in, err := l.Read(index)
	if err != nil {
		return err
	}
	log.Data = in.GetValue()
	log.Index = in.GetOffset()
	log.Type = raft.LogType(in.GetType())
	log.Term = in.GetTerm()
	return nil
}

// LastIndex implements raft.LogStore.
func (l *logStore) LastIndex() (uint64, error) {
	return l.HighestOffset()
}

// StoreLog implements raft.LogStore.
func (l *logStore) StoreLog(log *raft.Log) error {
	return l.StoreLogs([]*raft.Log{log})
}

// StoreLogs implements raft.LogStore.
func (l *logStore) StoreLogs(logs []*raft.Log) error {
	for _, log := range logs {
		if _, err := l.Append(&logv1.Record{
			Value: log.Data,
			Term:  log.Term,
			Type:  uint32(log.Type),
		}); err != nil {
			return err
		}
	}
	return nil
}
