package server

import (
	"context"
	"distributed-systems/internal/log"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

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
	var errOOR *log.ErrOffsetOutOfRange
	if errors.As(err, &errOOR) {
		return addErrOffsetOutOfRangeDetails(errOOR)
	}
	return err
}

func addErrOffsetOutOfRangeDetails(e *log.ErrOffsetOutOfRange) *connect.Error {
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
