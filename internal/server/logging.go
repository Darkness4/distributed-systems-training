package server

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func LoggingInterceptor(logger slog.Logger) connect.UnaryInterceptorFunc {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return connect.UnaryFunc(func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			now := time.Now()
			res, err := next(ctx, req)
			duration := time.Since(now)
			var code string
			if err != nil {
				code = connect.CodeOf(err).String()
			} else {
				code = "OK"
			}
			args := []any{
				slog.String("grpc.code", code),
				slog.String("grpc.start_time", now.Format(time.RFC3339)),
				slog.Int64("grpc.time_ns", duration.Nanoseconds()),
				slog.String(
					"peer.address",
					req.Peer().Addr,
				),
			}
			desc, ok := req.Spec().Schema.(protoreflect.MethodDescriptor)
			if !ok {
				args = append(args, slog.String("grpc.procedure", req.Spec().Procedure))
			} else {
				args = append(
					args,
					slog.String("grpc.method", string(desc.Name())),
					slog.String("grpc.service", string(desc.Parent().Name())),
				)
			}
			logger.Info("finished unary call", args...)
			return res, WrapToConnectError(err)
		})
	})
}
