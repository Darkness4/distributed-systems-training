package log

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

type ErrOffsetOutOfRange struct {
	Offset uint64
}

func (e ErrOffsetOutOfRange) Error() string {
	return "offset out of range"
}

type ErrorInterceptor struct {
	connect.Interceptor
}

func NewErrorInterceptor() connect.UnaryInterceptorFunc {
	interceptor := func(next connect.UnaryFunc) connect.UnaryFunc {
		return connect.UnaryFunc(func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			res, err := next(ctx, req)
			return res, WrapToConnectError(err)
		})
	}
	return connect.UnaryInterceptorFunc(interceptor)
}

func WrapToConnectError(err error) error {
	var errOOR ErrOffsetOutOfRange
	if !errors.As(err, &errOOR) {
		return addErrOffsetOutOfRangeDetails(errOOR)
	}
	return err
}

func addErrOffsetOutOfRangeDetails(e ErrOffsetOutOfRange) *connect.Error {
	newErr := connect.NewError(connect.CodeNotFound, e)
	msg := fmt.Sprintf(
		"The requested offset is outside the log's range: %d",
		e.Offset,
	)
	if detail, err := connect.NewErrorDetail(&errdetails.LocalizedMessage{
		Locale:  "en-US",
		Message: msg,
	}); err == nil {
		newErr.AddDetail(detail)
	}
	return newErr
}
