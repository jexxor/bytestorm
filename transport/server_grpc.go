package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"jexxor/bytestorm/api"
	"jexxor/bytestorm/core"
	"jexxor/bytestorm/infra"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	engineHeader            = "X-ByteStorm-Engine"
	sessionHeader           = "x-bytestorm-session-id"
	stopTimeout             = 5 * time.Second
	streamChunkPoolCapacity = 64 << 10
	streamChunkPoolMaxCap   = 16 << 20
)

var streamSessionSeq uint64

type SearchHandler struct {
	api.UnimplementedSearchServiceServer
	svc           *core.SearchService
	chunkPool     sync.Pool
	summaryWriter infra.StreamSummaryWriter
}

func NewSearchHandler(svc *core.SearchService, summaryWriter ...infra.StreamSummaryWriter) *SearchHandler {
	var writer infra.StreamSummaryWriter
	if len(summaryWriter) > 0 {
		writer = summaryWriter[0]
	}

	h := &SearchHandler{
		svc:           svc,
		summaryWriter: writer,
	}
	h.chunkPool.New = func() any {
		return make([]byte, 0, streamChunkPoolCapacity)
	}

	return h
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
		resp.Index = indices[0]
		resp.Found = true
	}

	return resp, nil
}

func (h *SearchHandler) StreamSearch(stream api.SearchService_StreamSearchServer) error {
	ctx := stream.Context()
	engineID := resolveEngineID(ctx)
	if engineID == "" {
		engineID = core.SIMDEngineID
	}
	sessionID := resolveSessionID(ctx)

	var pattern []byte
	var tail []byte
	var processed int64
	var streamMatchCount int64

	for {
		req, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return h.persistStreamSummary(ctx, sessionID, pattern, streamMatchCount)
			}
			return err
		}

		if req == nil {
			continue
		}

		incomingPattern := req.GetPattern()
		switch {
		case len(incomingPattern) > 0 && len(pattern) == 0:
			pattern = append([]byte(nil), incomingPattern...)
		case len(incomingPattern) > 0 && !bytes.Equal(pattern, incomingPattern):
			return status.Error(codes.InvalidArgument, "pattern must remain constant throughout StreamSearch")
		case len(pattern) == 0:
			return status.Error(codes.InvalidArgument, "pattern is required in the first streamed request")
		}

		text := req.GetText()
		if len(text) == 0 {
			continue
		}

		overlapLen := len(tail)
		chunk := h.acquireChunkBuffer(overlapLen + len(text))
		copy(chunk, tail)
		copy(chunk[overlapLen:], text)

		searchStart := time.Now()
		matches, searchErr := h.svc.Lookup(ctx, chunk, pattern, engineID)
		searchLatency := time.Since(searchStart)
		if searchErr != nil {
			h.releaseChunkBuffer(chunk)
			return mapLookupError(searchErr)
		}

		baseOffset := processed - int64(overlapLen)
		minNewStart := processed - int64(len(pattern)) + 1
		if minNewStart < 0 {
			minNewStart = 0
		}

		chunkMatchCount := 0
		for _, idx := range matches {
			absolute := baseOffset + idx
			if absolute < minNewStart {
				continue
			}

			if sendErr := stream.Send(&api.LookupResponse{
				Index: absolute,
				Found: true,
			}); sendErr != nil {
				h.releaseChunkBuffer(chunk)
				return sendErr
			}

			chunkMatchCount++
		}

		streamMatchCount += int64(chunkMatchCount)
		if engineID == core.SIMDEngineID {
			infra.ObserveSIMDChunk(len(chunk), chunkMatchCount, searchLatency)
		}

		processed += int64(len(text))

		tailLen := len(pattern) - 1
		if tailLen > 0 {
			if tailLen > len(chunk) {
				tailLen = len(chunk)
			}
			if cap(tail) < tailLen {
				tail = make([]byte, tailLen)
			} else {
				tail = tail[:tailLen]
			}
			copy(tail, chunk[len(chunk)-tailLen:])
		} else {
			tail = tail[:0]
		}

		h.releaseChunkBuffer(chunk)
	}
}

func (h *SearchHandler) persistStreamSummary(ctx context.Context, sessionID string, pattern []byte, matchCount int64) error {
	if h.summaryWriter == nil || !h.summaryWriter.Enabled() || len(pattern) == 0 {
		return nil
	}

	err := h.summaryWriter.BulkUpsertStreamSummary(ctx, infra.StreamSummary{
		SessionID:  sessionID,
		Timestamp:  time.Now().UTC(),
		Pattern:    append([]byte(nil), pattern...),
		MatchCount: matchCount,
	})
	if err != nil {
		return status.Errorf(codes.Internal, "failed to persist stream summary: %v", err)
	}

	return nil
}

func (h *SearchHandler) acquireChunkBuffer(size int) []byte {
	if size <= 0 {
		return nil
	}

	raw := h.chunkPool.Get()
	buf, _ := raw.([]byte)
	if cap(buf) < size {
		return make([]byte, size)
	}

	return buf[:size]
}

func (h *SearchHandler) releaseChunkBuffer(buf []byte) {
	if buf == nil {
		return
	}
	if cap(buf) > streamChunkPoolMaxCap {
		return
	}

	h.chunkPool.Put(buf[:0])
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

func resolveSessionID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		sessions := md.Get(sessionHeader)
		if len(sessions) > 0 && sessions[0] != "" {
			return sessions[0]
		}
	}

	seq := atomic.AddUint64(&streamSessionSeq, 1)
	return fmt.Sprintf("stream-%d-%d", time.Now().UTC().UnixNano(), seq)
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

func NewGRPCServer(host string, port int, svc *core.SearchService, summaryWriter ...infra.StreamSummaryWriter) *GRPCServer {
	addr := fmt.Sprintf("%s:%d", host, port)
	grpcServer := grpc.NewServer()
	api.RegisterSearchServiceServer(grpcServer, NewSearchHandler(svc, summaryWriter...))

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
