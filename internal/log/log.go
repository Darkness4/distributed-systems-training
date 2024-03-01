package log

import (
	logv1 "distributed-systems/gen/log/v1"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type Log struct {
	mu  sync.RWMutex
	Dir string
	Config

	activeSegment *segment
	segments      []*segment
}

func NewLog(dir string, c Config) *Log {
	if c.Segment.MaxIndexBytes == 0 {
		c.Segment.MaxIndexBytes = 1024
	}
	if c.Segment.MaxStoreBytes == 0 {
		c.Segment.MaxStoreBytes = 1024
	}
	return (&Log{
		Dir:    dir,
		Config: c,
	}).setup()
}

func (l *Log) setup() *Log {
	files, err := os.ReadDir(l.Dir)
	if err != nil {
		panic(err)
	}
	var baseOffsets []uint64
	for _, file := range files {
		offStr := strings.TrimSuffix(file.Name(), path.Ext(file.Name()))
		off, _ := strconv.ParseUint(offStr, 10, 0)
		baseOffsets = append(baseOffsets, off)
	}
	slices.Sort(baseOffsets)
	for _, off := range baseOffsets {
		l.newSegment(off)
	}
	if l.segments == nil {
		l.newSegment(l.Config.Segment.InitialOffset)
	}
	return l
}

func (l *Log) newSegment(off uint64) {
	s := newSegment(l.Dir, off, l.Config)
	l.segments = append(l.segments, s)
	l.activeSegment = s
}

func (l *Log) Append(record *logv1.Record) (uint64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	off, err := l.activeSegment.Append(record)
	if err != nil {
		return 0, err
	}
	if l.activeSegment.IsMaxed() {
		l.newSegment(off + 1)
	}
	return off, nil
}

func (l *Log) Read(off uint64) (*logv1.Record, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var s *segment
	for _, segment := range l.segments {
		if segment.baseOffset <= off && off < segment.nextOffset {
			s = segment
			break
		}
	}
	if s == nil || s.nextOffset <= off {
		return nil, fmt.Errorf("offset out of range: %d", off)
	}
	return s.Read(off)
}

func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, segment := range l.segments {
		if err := segment.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (l *Log) Remove() error {
	if err := l.Close(); err != nil {
		return err
	}
	return os.RemoveAll(l.Dir)
}

func (l *Log) Reset() error {
	if err := l.Remove(); err != nil {
		return err
	}
	_ = l.setup()
	return nil
}

func (l *Log) LowestOffset() (uint64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.segments[0].baseOffset, nil
}

func (l *Log) HighestOffset() (uint64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	off := l.segments[len(l.segments)-1].nextOffset
	if off == 0 {
		return 0, nil
	}
	return off - 1, nil
}

// Truncate removes all segments whose highest offset is lower than lowest.
func (l *Log) Truncate(lowest uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var segments []*segment
	for _, s := range l.segments {
		if s.nextOffset <= lowest+1 {
			if err := s.Remove(); err != nil {
				return err
			}
			continue
		}
		segments = append(segments, s)
	}
	l.segments = segments
	return nil
}

// Reader returns a new io.Reader to read the whole log.
func (l *Log) Reader() io.Reader {
	l.mu.RLock()
	defer l.mu.RUnlock()
	readers := make([]io.Reader, len(l.segments))
	for i, segment := range l.segments {
		readers[i] = &originReader{segment.store, 0}
	}
	return io.MultiReader(readers...)
}

type originReader struct {
	*store
	off int64
}

func (o *originReader) Read(p []byte) (int, error) {
	n, err := o.ReadAt(p, o.off)
	o.off += int64(n)
	return n, err
}
