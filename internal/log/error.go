package log

var _ error = ErrOffsetOutOfRange{}

type ErrOffsetOutOfRange struct {
	Offset uint64
}

func (e ErrOffsetOutOfRange) Error() string {
	return "offset out of range"
}
