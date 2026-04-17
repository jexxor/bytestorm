package transport

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"time"

	"jexxor/bytestorm/api"
	"jexxor/bytestorm/core"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	engineHeader = "X-ByteStorm-Engine"
	stopTimeout  = 5 * time.Second
)

type SearchHandler struct {
	api.UnimplementedSearchServiceServer
	svc *core.SearchService
}

func NewSearchHandler(svc *core.SearchService) *SearchHandler {
	return &SearchHandler{svc: svc}
}

func (h *SearchHandler) Lookup(ctx context.Context, req *api.LookupRequest) (*api.LookupResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	engineID := resolveEngineID(ctx)

	indices, err := h.svc.Lookup(ctx, req.GetText(), req.GetPattern(), engineID)
	if err != nil {
		return nil, mapLookupError(err)
	}

	resp := &api.LookupResponse{
		Index: -1,
		Found: false,
	}

	if len(indices) > 0 {
		if indices[0] > math.MaxInt32 {
			return nil, status.Error(codes.OutOfRange, "match index exceeds int32 range")
		}

		resp.Index = int32(indices[0])
		resp.Found = true
	}

	return resp, nil
}

func resolveEngineID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	engines := md.Get(engineHeader)
	if len(engines) == 0 {
		return ""
	}

	return engines[0]
}

func mapLookupError(err error) error {
	var noEngineErr *core.ErrNoEngine
	if errors.As(err, &noEngineErr) {
		return status.Error(codes.NotFound, noEngineErr.Error())
	}

	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, err.Error())
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, err.Error())
	}

	return status.Error(codes.Internal, err.Error())
}

type GRPCServer struct {
	addr       string
	grpcServer *grpc.Server
	listener   net.Listener
}

func NewGRPCServer(host string, port int, svc *core.SearchService) *GRPCServer {
	addr := fmt.Sprintf("%s:%d", host, port)
	grpcServer := grpc.NewServer()
	api.RegisterSearchServiceServer(grpcServer, NewSearchHandler(svc))

	return &GRPCServer{
		addr:       addr,
		grpcServer: grpcServer,
	}
}

func (s *GRPCServer) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}

	s.listener = listener
	zap.S().Infof("gRPC server listening on %s", s.addr)

	errCh := make(chan error, 1)
	go func() {
		err := s.grpcServer.Serve(listener)
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errCh <- err
			return
		}
		close(errCh)
	}()

	select {
	case err, ok := <-errCh:
		if !ok {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		defer cancel()
		if stopErr := s.Stop(shutdownCtx); stopErr != nil {
			return stopErr
		}
		return nil
	}
}

func (s *GRPCServer) Stop(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		if s.listener != nil {
			_ = s.listener.Close()
			s.listener = nil
		}
		return nil
	case <-ctx.Done():
		s.grpcServer.Stop()
		if s.listener != nil {
			_ = s.listener.Close()
			s.listener = nil
		}
		return ctx.Err()
	}
}
