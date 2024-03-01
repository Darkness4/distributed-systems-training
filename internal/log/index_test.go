package log

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func prepareIndex(c Config) *index {
	// Arrange
	f, err := os.CreateTemp("", "index")
	if err != nil {
		panic(err)
	}

	return newIndex(f, c)
}

func TestIndex(t *testing.T) {
	// Arrange
	config := Config{
		Segment: Segment{
			MaxIndexBytes: 1024,
		},
	}
	idx := prepareIndex(config)
	fname := idx.file.Name()
	defer os.Remove(fname)

	_, _, err := idx.Read(-1)
	require.Error(t, err)

	// Act: Write and read
	entries := []struct {
		Off uint32
		Pos uint64
	}{
		{Off: 0, Pos: 0},
		{Off: 1, Pos: 10},
	}

	for _, want := range entries {
		err = idx.Write(want.Off, want.Pos)
		require.NoError(t, err)

		_, pos, err := idx.Read(int64(want.Off))
		require.NoError(t, err)
		require.Equal(t, want.Pos, pos)
	}

	// index and scanner should error when reading past existing entries
	_, _, err = idx.Read(int64(len(entries)))
	require.Equal(t, io.EOF, err)
	_ = idx.Close()

	// index should build its state from the existing file
	f, err := os.OpenFile(fname, os.O_RDWR, 0600)
	require.NoError(t, err)
	idx = newIndex(f, config)
	defer idx.Close()
	require.NoError(t, err)
	off, pos, err := idx.Read(-1)
	require.NoError(t, err)
	require.Equal(t, uint32(1), off)
	require.Equal(t, entries[1].Pos, pos)
}
