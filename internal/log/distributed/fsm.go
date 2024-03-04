package distributed

import (
	"bytes"
	logv1 "distributed-systems/gen/log/v1"
	"distributed-systems/internal/log"
	"io"

	"github.com/hashicorp/raft"
	"google.golang.org/protobuf/proto"
)

type RequestType uint8

const (
	AppendRequestType RequestType = iota
)

var _ raft.FSM = (*fsm)(nil)

type fsm struct {
	log *log.Log
}

// Apply implements raft.FSM.
func (f *fsm) Apply(record *raft.Log) interface{} {
	buf := record.Data
	reqType := RequestType(buf[0])
	switch reqType {
	case AppendRequestType:
		return f.applyAppend(buf[1:])
	}
	return nil
}

func (f *fsm) applyAppend(b []byte) interface{} {
	var req logv1.ProduceRequest
	err := proto.Unmarshal(b, &req)
	if err != nil {
		return err
	}
	offset, err := f.log.Append(req.Record)
	if err != nil {
		return err
	}
	return &logv1.ProduceResponse{Offset: offset}
}

// Restore implements raft.FSM.
func (f *fsm) Restore(r io.ReadCloser) error {
	b := make([]byte, log.LenWidth)
	var buf bytes.Buffer
	for i := 0; ; i++ {
		_, err := io.ReadFull(r, b)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		size := int64(log.Encoding.Uint64(b))
		if _, err = io.CopyN(&buf, r, int64(size)); err != nil {
			return err
		}
		record := &logv1.Record{}
		if err := proto.Unmarshal(buf.Bytes(), record); err != nil {
			return err
		}
		if i == 0 {
			f.log.Config.Segment.InitialOffset = record.Offset
			if err := f.log.Reset(); err != nil {
				return err
			}
		}
		if _, err = f.log.Append(record); err != nil {
			return err
		}
		buf.Reset()
	}
	return nil
}

// Snapshot implements raft.FSM.
func (f *fsm) Snapshot() (raft.FSMSnapshot, error) {
	r := f.log.Reader()
	return &snapshot{reader: r}, nil
}
