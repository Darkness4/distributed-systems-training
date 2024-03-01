package log

import (
	logv1 "distributed-systems/gen/log/v1"
	"fmt"
	"os"
	"path"

	"google.golang.org/protobuf/proto"
)

type segment struct {
	store                  *store
	index                  *index
	baseOffset, nextOffset uint64
	config                 Config
}

func newSegment(dir string, baseOffset uint64, c Config) *segment {
	s := segment{
		baseOffset: baseOffset,
		config:     c,
	}
	storeFile, err := os.OpenFile(
		path.Join(dir, fmt.Sprintf("%d.store", baseOffset)),
		os.O_CREATE|os.O_RDWR|os.O_APPEND,
		0644,
	)
	if err != nil {
		panic(err)
	}
	s.store = newStore(storeFile)
	indexFile, err := os.OpenFile(
		path.Join(dir, fmt.Sprintf("%d.index", baseOffset)),
		os.O_CREATE|os.O_RDWR,
		0644,
	)
	if err != nil {
		panic(err)
	}
	s.index = newIndex(indexFile, c)
	if off, _, err := s.index.Read(-1); err != nil {
		s.nextOffset = baseOffset
	} else {
		s.nextOffset = baseOffset + uint64(off) + 1
	}

	return &s
}

func (s *segment) Append(record *logv1.Record) (uint64, error) {
	cur := s.nextOffset
	p, err := proto.Marshal(record)
	if err != nil {
		return 0, err
	}
	_, pos, err := s.store.Append(p)
	if err != nil {
		return 0, err
	}
	if err = s.index.Write(uint32(s.nextOffset-uint64(s.baseOffset)), pos); err != nil {
		return 0, err
	}
	s.nextOffset++
	return cur, nil
}

func (s *segment) Read(off uint64) (*logv1.Record, error) {
	_, pos, err := s.index.Read(int64(off - s.baseOffset))
	if err != nil {
		return nil, err
	}
	p, err := s.store.Read(pos)
	if err != nil {
		return nil, err
	}
	var record logv1.Record
	if err = proto.Unmarshal(p, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *segment) IsMaxed() bool {
	return s.store.size >= s.config.Segment.MaxStoreBytes ||
		s.index.size >= s.config.Segment.MaxIndexBytes
}

func (s *segment) Remove() error {
	if err := s.Close(); err != nil {
		return err
	}
	if err := os.Remove(s.index.Name()); err != nil {
		return err
	}
	return os.Remove(s.store.Name())
}

func (s *segment) Close() error {
	if err := s.index.Close(); err != nil {
		return err
	}
	return s.store.Close()
}

func nearestMultiple(j, k uint64) uint64 {
	return ((j - k + 1) / k) * k
}
