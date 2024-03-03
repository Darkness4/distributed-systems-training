package server

import (
	"context"
	logv1 "distributed-systems/gen/log/v1"
	"distributed-systems/gen/log/v1/logv1connect"
	"distributed-systems/internal/log"
	"errors"
	"io"
	"net/http"

	"connectrpc.com/connect"
)

type CommitLog interface {
	Append(*logv1.Record) (uint64, error)
	Read(uint64) (*logv1.Record, error)
}

type Config struct {
	CommitLog
}

var _ logv1connect.LogAPIHandler = (*LogAPIHandler)(nil)

type LogAPIHandler struct {
	*Config
}

func NewLogAPIHandler(config *Config, opts ...connect.HandlerOption) (string, http.Handler) {
	if config == nil {
		panic("missing config")
	}
	opts = append(opts, connect.WithInterceptors(
		connect.Interceptor(NewErrorInterceptor()),
	))
	path, handler := logv1connect.NewLogAPIHandler(&LogAPIHandler{
		Config: config,
	}, opts...)
	return path, handler
}

func (s *LogAPIHandler) Consume(
	ctx context.Context,
	req *connect.Request[logv1.ConsumeRequest],
) (*connect.Response[logv1.ConsumeResponse], error) {
	record, err := s.CommitLog.Read(req.Msg.Offset)
	if err != nil {
		return nil, err
	}
	return &connect.Response[logv1.ConsumeResponse]{
		Msg: &logv1.ConsumeResponse{
			Record: record,
		},
	}, nil
}

func (s *LogAPIHandler) ConsumeStream(
	ctx context.Context,
	req *connect.Request[logv1.ConsumeStreamRequest],
	stream *connect.ServerStream[logv1.ConsumeStreamResponse],
) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			res, err := s.Consume(ctx, &connect.Request[logv1.ConsumeRequest]{
				Msg: &logv1.ConsumeRequest{
					Offset: req.Msg.Offset,
				},
			})
			if errors.Is(err, &log.ErrOffsetOutOfRange{}) {
				continue
			} else if err != nil {
				return connect.NewError(connect.CodeInternal, err)
			}
			if err := stream.Send(&logv1.ConsumeStreamResponse{
				Record: res.Msg.Record,
			}); err != nil {
				return connect.NewError(connect.CodeInternal, err)
			}
			req.Msg.Offset++
		}
	}
}

func (s *LogAPIHandler) Produce(
	ctx context.Context,
	req *connect.Request[logv1.ProduceRequest],
) (*connect.Response[logv1.ProduceResponse], error) {
	offset, err := s.CommitLog.Append(req.Msg.GetRecord())
	if err != nil {
		return nil, err
	}
	return &connect.Response[logv1.ProduceResponse]{
		Msg: &logv1.ProduceResponse{
			Offset: offset,
		},
	}, nil
}

func (s *LogAPIHandler) ProduceStream(
	ctx context.Context,
	stream *connect.BidiStream[logv1.ProduceStreamRequest, logv1.ProduceStreamResponse],
) error {
	for {
		req, err := stream.Receive()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return connect.NewError(connect.CodeUnknown, err)
		}
		res, err := s.Produce(ctx, &connect.Request[logv1.ProduceRequest]{
			Msg: &logv1.ProduceRequest{
				Record: req.Record,
			},
		})
		if err != nil {
			return connect.NewError(connect.CodeInternal, err)
		}
		if err := stream.Send(&logv1.ProduceStreamResponse{
			Offset: res.Msg.Offset,
		}); err != nil {
			return connect.NewError(connect.CodeInternal, err)
		}
	}
}
