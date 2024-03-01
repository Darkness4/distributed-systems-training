package log

import (
	"bufio"
	"encoding/binary"
	"os"
	"sync"
)

var (
	enc = binary.BigEndian
)

const (
	lenWidth = 8
)

type store struct {
	*os.File
	mu   sync.Mutex
	buf  *bufio.Writer
	size uint64
}

func newStore(f *os.File) *store {
	fi, err := f.Stat()
	if err != nil {
		panic(err)
	}
	return &store{
		File: f,
		buf:  bufio.NewWriter(f),
		size: uint64(fi.Size()),
	}
}

// Append writes the record to the store and returns the position at which the record was written.
//
// We manually pack the data.
// The first 8 bytes will be the length of the record.
// The rest will be the record itself.
func (s *store) Append(p []byte) (n uint64, pos uint64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pos = s.size
	// Write the length of the record
	if err = binary.Write(s.buf, enc, uint64(len(p))); err != nil {
		return
	}
	// Write the record
	w, err := s.buf.Write(p)
	if err != nil {
		return
	}
	w += lenWidth
	s.size += uint64(w)
	return uint64(w), pos, nil
}

// Read reads the record at the given position.
//
// We manually unpack the data.
// The first 8 bytes will be the length of the record.
// The rest will be the record itself.
func (s *store) Read(pos uint64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.buf.Flush(); err != nil {
		return nil, err
	}
	size := make([]byte, lenWidth)
	if _, err := s.File.ReadAt(size, int64(pos)); err != nil {
		return nil, err
	}
	b := make([]byte, enc.Uint64(size))
	if _, err := s.File.ReadAt(b, int64(pos+lenWidth)); err != nil {
		return nil, err
	}
	return b, nil
}

// ReadAt reads len(p) bytes into p beginning at the byte offset off.
//
// This method is implemented to satisfy the io.ReaderAt interface.
func (s *store) ReadAt(p []byte, off int64) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.buf.Flush(); err != nil {
		return 0, err
	}
	return s.File.ReadAt(p, off)
}

// Close closes the store.
func (s *store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.buf.Flush(); err != nil {
		return err
	}
	return s.File.Close()
}
