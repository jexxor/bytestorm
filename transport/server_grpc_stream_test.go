package transport

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"jexxor/bytestorm/api"
	"jexxor/bytestorm/core"
	"jexxor/bytestorm/infra"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

const grpcBufconnSize = 1024 * 1024

func TestStreamSearchOverlapAndRealtime(t *testing.T) {
	t.Parallel()

	client, cleanup := newTestSearchClient(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(engineHeader, core.SIMDEngineID))

	stream, err := client.StreamSearch(ctx)
	if err != nil {
		t.Fatalf("StreamSearch returned error: %v", err)
	}

	if err := stream.Send(&api.LookupRequest{Pattern: []byte("needle"), Text: []byte("xxneed")}); err != nil {
		t.Fatalf("stream.Send first chunk returned error: %v", err)
	}
	if err := stream.Send(&api.LookupRequest{Text: []byte("lezz")}); err != nil {
		t.Fatalf("stream.Send second chunk returned error: %v", err)
	}

	first, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv first response returned error: %v", err)
	}
	if !first.GetFound() || first.GetIndex() != 2 {
		t.Fatalf("first streamed response = %+v, want found=true index=2", first)
	}

	if err := stream.Send(&api.LookupRequest{Text: []byte("yyneedle")}); err != nil {
		t.Fatalf("stream.Send third chunk returned error: %v", err)
	}

	second, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv second response returned error: %v", err)
	}
	if !second.GetFound() || second.GetIndex() != 12 {
		t.Fatalf("second streamed response = %+v, want found=true index=12", second)
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend returned error: %v", err)
	}

	_, err = stream.Recv()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("final stream.Recv() error = %v, want io.EOF", err)
	}
}

func TestStreamSearchPersistsSummaryOnClose(t *testing.T) {
	t.Parallel()

	writer := &stubSummaryWriter{enabled: true}
	client, cleanup := newTestSearchClient(t, writer)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(
		engineHeader,
		core.SIMDEngineID,
		sessionHeader,
		"session-42",
	))

	stream, err := client.StreamSearch(ctx)
	if err != nil {
		t.Fatalf("StreamSearch returned error: %v", err)
	}

	if err := stream.Send(&api.LookupRequest{Pattern: []byte("needle"), Text: []byte("xxneedlezz")}); err != nil {
		t.Fatalf("stream.Send returned error: %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv returned error: %v", err)
	}
	if !resp.GetFound() || resp.GetIndex() != 2 {
		t.Fatalf("streamed response = %+v, want found=true index=2", resp)
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend returned error: %v", err)
	}

	_, err = stream.Recv()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("final stream.Recv() error = %v, want io.EOF", err)
	}

	summaries := writer.Summaries()
	if len(summaries) != 1 {
		t.Fatalf("summary count = %d, want 1", len(summaries))
	}

	summary := summaries[0]
	if summary.SessionID != "session-42" {
		t.Fatalf("summary session_id = %q, want %q", summary.SessionID, "session-42")
	}
	if summary.MatchCount != 1 {
		t.Fatalf("summary match_count = %d, want 1", summary.MatchCount)
	}
	if string(summary.Pattern) != "needle" {
		t.Fatalf("summary pattern = %q, want %q", summary.Pattern, "needle")
	}
}

func newTestSearchClient(t *testing.T, summaryWriter ...infra.StreamSummaryWriter) (api.SearchServiceClient, func()) {
	t.Helper()

	svc := core.NewSearchService(core.SIMDEngineID)
	svc.RegisterEngine(core.NewSIMDEngine(0))
	svc.RegisterEngine(core.NewKMPEngine())
	svc.RegisterEngine(core.NewStdlibEngine())

	grpcServer := grpc.NewServer()
	api.RegisterSearchServiceServer(grpcServer, NewSearchHandler(svc, summaryWriter...))

	listener := bufconn.Listen(grpcBufconnSize)
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			_ = err
		}
	}()

	dialer := func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(
		ctx,
		"bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		listener.Close()
		grpcServer.Stop()
		t.Fatalf("grpc.DialContext returned error: %v", err)
	}

	cleanup := func() {
		_ = conn.Close()
		grpcServer.Stop()
		_ = listener.Close()
	}

	return api.NewSearchServiceClient(conn), cleanup
}

type stubSummaryWriter struct {
	mu        sync.Mutex
	enabled   bool
	summaries []infra.StreamSummary
}

func (w *stubSummaryWriter) BulkUpsertStreamSummary(_ context.Context, summary infra.StreamSummary) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	copySummary := infra.StreamSummary{
		SessionID:  summary.SessionID,
		Timestamp:  summary.Timestamp,
		Pattern:    append([]byte(nil), summary.Pattern...),
		MatchCount: summary.MatchCount,
	}
	w.summaries = append(w.summaries, copySummary)

	return nil
}

func (w *stubSummaryWriter) Enabled() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.enabled
}

func (w *stubSummaryWriter) Close(context.Context) error {
	return nil
}

func (w *stubSummaryWriter) Summaries() []infra.StreamSummary {
	w.mu.Lock()
	defer w.mu.Unlock()

	out := make([]infra.StreamSummary, len(w.summaries))
	copy(out, w.summaries)
	return out
}
