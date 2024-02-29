package server

import (
	"errors"
	"sync"
)

var ErrOffsetNotFound = errors.New("offset not found")

type Record struct {
	Value  []byte `json:"value"`
	Offset uint64 `json:"offset"`
}

type Log struct {
	mu     sync.Mutex
	record []Record
}

func NewLog() *Log {
	return &Log{}
}

func (l *Log) Append(record Record) (uint64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	record.Offset = uint64(len(l.record))
	l.record = append(l.record, record)
	return record.Offset, nil
}

func (l *Log) Read(offset uint64) (Record, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if offset >= uint64(len(l.record)) {
		return Record{}, ErrOffsetNotFound
	}
	return l.record[offset], nil
}
