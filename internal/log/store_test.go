package log

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// Fixtures for the packed data.
var (
	write = []byte("write")
	width = uint64(lenWidth + len(write))
)

func prepareStore() *store {
	// Arrange
	f, err := os.CreateTemp("", "store")
	if err != nil {
		panic(err)
	}

	store, err := newStore(f)
	if err != nil {
		panic(err)
	}
	return store
}

func deleteStore(s *store) {
	if err := s.Close(); err != nil {
		panic(err)
	}
	os.Remove(s.Name())
}

func TestStore(t *testing.T) {
	// Arrange
	s := prepareStore()
	defer deleteStore(s)

	// Act & Assert
	testAppend(t, s)
	testRead(t, s)
	testReadAt(t, s)
}

func testAppend(t *testing.T, s *store) {
	t.Helper()
	for i := uint64(0); i < 3; i++ {
		n, pos, err := s.Append(write)
		require.NoError(t, err)
		require.Equal(t, i*width, pos)
		require.Equal(t, width, n)
	}
}

func testRead(t *testing.T, s *store) {
	t.Helper()
	for i, pos := 1, uint64(0); i < 4; i, pos = i+1, pos+width {
		// Act
		p, err := s.Read(pos)
		require.NoError(t, err)
		require.Equal(t, write, p)
	}
}

func testReadAt(t *testing.T, s *store) {
	t.Helper()
	for i, off := uint64(1), int64(0); i < 4; i++ {
		// Read size
		b := make([]byte, lenWidth)
		n, err := s.ReadAt(b, off)
		require.NoError(t, err)
		require.Equal(t, lenWidth, n)
		off += int64(n)

		// Read record
		size := enc.Uint64(b)
		b = make([]byte, size)
		n, err = s.ReadAt(b, off)
		require.NoError(t, err)
		require.Equal(t, int(size), n)
		off += int64(n)
	}
}
