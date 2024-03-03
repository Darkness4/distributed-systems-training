package log

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
)

var (
	offWidth   uint64 = 4
	posWidth   uint64 = 8
	entryWidth        = offWidth + posWidth
)

type index struct {
	file *os.File
	mmap []byte
	size uint64
}

func newIndex(f *os.File, c Config) *index {
	if f == nil {
		panic("nil file")
	}
	idx := &index{
		file: f,
	}
	fi, err := f.Stat()
	if err != nil {
		panic(err)
	}
	idx.size = uint64(fi.Size())

	// Allocate the memory for the index.
	if err = f.Truncate(int64(c.Segment.MaxIndexBytes)); err != nil {
		panic(err)
	}
	if idx.mmap, err = syscall.Mmap(
		int(f.Fd()),
		0,
		int(c.Segment.MaxIndexBytes),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED,
	); err != nil {
		panic(err)
	}
	return idx
}

func (i *index) Close() error {
	if _, _, err := syscall.Syscall(syscall.SYS_MSYNC, uintptr(unsafe.Pointer(&i.mmap[0])), uintptr(i.size), uintptr(syscall.MS_SYNC)); err != 0 {
		return fmt.Errorf("msync: %w", err)
	}
	if err := syscall.Munmap(i.mmap); err != nil {
		return fmt.Errorf("unmap: %w", err)
	}
	if err := i.file.Sync(); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	// Truncate to the true size of the index.
	if err := i.file.Truncate(int64(i.size)); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}
	return i.file.Close()
}

func (i *index) Read(in int64) (out uint32, pos uint64, err error) {
	if i.size == 0 {
		return 0, 0, io.EOF
	}
	if in == -1 {
		out = uint32((i.size / entryWidth) - 1)
	} else {
		out = uint32(in)
	}
	pos = uint64(out) * entryWidth
	if i.size < pos+entryWidth {
		return 0, 0, io.EOF
	}
	// 4 bytes for the offset and 8 bytes for the position.
	out = enc.Uint32(i.mmap[pos : pos+offWidth])
	pos = enc.Uint64(i.mmap[pos+offWidth : pos+entryWidth])
	return out, pos, nil
}

func (i *index) Write(off uint32, pos uint64) error {
	if uint64(len(i.mmap)) < i.size+entryWidth {
		return io.EOF
	}
	// 4 bytes for the offset and 8 bytes for the position.
	enc.PutUint32(i.mmap[i.size:i.size+offWidth], off)
	enc.PutUint64(i.mmap[i.size+offWidth:i.size+entryWidth], pos)
	i.size += entryWidth
	return nil
}

func (i *index) Name() string {
	return i.file.Name()
}
