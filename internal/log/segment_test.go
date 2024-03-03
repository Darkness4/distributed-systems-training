package log

import (
	logv1 "distributed-systems/gen/log/v1"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSegment(t *testing.T) {
	dir, err := os.MkdirTemp("", "segment")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	c := Config{
		Segment: Segment{
			MaxIndexBytes: entryWidth * 3,
			MaxStoreBytes: 1024,
			InitialOffset: 16,
		},
	}

	want := &logv1.Record{Value: []byte("hello world")}

	s, err := newSegment(dir, c.InitialOffset, c)
	require.NoError(t, err)
	require.Equal(t, uint64(16), s.nextOffset, s.nextOffset)
	require.False(t, s.IsMaxed())

	for i := uint64(0); i < 3; i++ {
		off, err := s.Append(want)
		require.NoError(t, err)
		require.Equal(t, 16+i, off)

		got, err := s.Read(off)
		require.NoError(t, err)
		require.Equal(t, want.Value, got.Value)
	}

	_, err = s.Append(want)
	require.Equal(t, io.EOF, err)

	// maxed index
	require.True(t, s.IsMaxed())

	c.Segment.MaxStoreBytes = uint64(len(want.Value) * 3)
	c.Segment.MaxIndexBytes = 1024

	s, err = newSegment(dir, 16, c)
	require.NoError(t, err)
	// maxed store
	require.True(t, s.IsMaxed())

	err = s.Remove()
	require.NoError(t, err)
	s, err = newSegment(dir, 16, c)
	require.NoError(t, err)
	require.False(t, s.IsMaxed())
}
