package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"jexxor/bytestorm/api"
	"jexxor/bytestorm/core"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const (
	grpcBenchChunkSize      = 1 << 20
	grpcBenchTotalBytes     = 1 << 30
	grpcBenchChunkCount     = grpcBenchTotalBytes / grpcBenchChunkSize
	grpcBenchRequestTimeout = 3 * time.Minute
)

func BenchmarkStreamSearchNetwork(b *testing.B) {
	pattern := []byte("needle")
	chunk := buildGRPCBenchChunk(grpcBenchChunkSize, pattern)

	b.Run("grpc_stream_1gb_total", func(b *testing.B) {
		benchmarkGRPCStreamSearchNetwork(b, chunk, pattern, grpcBenchChunkCount)
	})

	b.Run("local_simd_search_1gb_total", func(b *testing.B) {
		benchmarkLocalSIMDSearch(b, chunk, pattern, grpcBenchChunkCount)
	})
}

func benchmarkGRPCStreamSearchNetwork(b *testing.B, chunk []byte, pattern []byte, chunksPerRun int) {
	svc := core.NewSearchService(core.SIMDEngineID)
	svc.RegisterEngine(core.NewSIMDEngine(0))

	grpcServer := grpc.NewServer()
	api.RegisterSearchServiceServer(grpcServer, NewSearchHandler(svc))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("net.Listen returned error: %v", err)
	}

	serveErrCh := make(chan error, 1)
	go func() {
		err := grpcServer.Serve(listener)
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			serveErrCh <- err
		}
		close(serveErrCh)
	}()

	b.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
		if serveErr, ok := <-serveErrCh; ok && serveErr != nil {
			b.Errorf("gRPC server stopped with error: %v", serveErr)
		}
	})

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	conn, err := grpc.DialContext(
		dialCtx,
		listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	dialCancel()
	if err != nil {
		b.Fatalf("grpc.DialContext returned error: %v", err)
	}
	b.Cleanup(func() {
		_ = conn.Close()
	})

	client := api.NewSearchServiceClient(conn)
	firstChunkReq := &api.LookupRequest{Pattern: pattern, Text: chunk}
	nextChunkReq := &api.LookupRequest{Text: chunk}

	b.ReportAllocs()
	b.SetBytes(grpcBenchTotalBytes)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		requestCtx, requestCancel := context.WithTimeout(context.Background(), grpcBenchRequestTimeout)
		requestCtx = metadata.NewOutgoingContext(requestCtx, metadata.Pairs(engineHeader, core.SIMDEngineID))

		stream, err := client.StreamSearch(requestCtx)
		if err != nil {
			requestCancel()
			b.Fatalf("StreamSearch returned error: %v", err)
		}

		if err := stream.Send(firstChunkReq); err != nil {
			requestCancel()
			b.Fatalf("stream.Send first chunk returned error: %v", err)
		}

		for chunkIndex := 1; chunkIndex < chunksPerRun; chunkIndex++ {
			if err := stream.Send(nextChunkReq); err != nil {
				requestCancel()
				b.Fatalf("stream.Send chunk %d returned error: %v", chunkIndex, err)
			}
		}

		if err := stream.CloseSend(); err != nil {
			requestCancel()
			b.Fatalf("stream.CloseSend returned error: %v", err)
		}

		responseCount := 0
		for {
			_, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				requestCancel()
				b.Fatalf("stream.Recv returned error: %v", err)
			}
			responseCount++
		}

		requestCancel()
		if responseCount == 0 {
			b.Fatal("expected at least one streamed match")
		}
	}
}

func benchmarkLocalSIMDSearch(b *testing.B, chunk []byte, pattern []byte, chunksPerRun int) {
	engine := core.NewSIMDEngine(0)
	ctx := context.Background()

	b.ReportAllocs()
	b.SetBytes(grpcBenchTotalBytes)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		totalMatches := 0
		for chunkIndex := 0; chunkIndex < chunksPerRun; chunkIndex++ {
			matches, err := engine.Search(ctx, chunk, pattern)
			if err != nil {
				b.Fatalf("SIMDEngine.Search returned error: %v", err)
			}
			totalMatches += len(matches)
		}

		if totalMatches == 0 {
			b.Fatal("expected at least one local match")
		}
	}
}

func buildGRPCBenchChunk(size int, pattern []byte) []byte {
	if size < len(pattern) {
		size = len(pattern)
	}

	chunk := bytes.Repeat([]byte("z"), size)
	offset := size / 2
	if offset+len(pattern) > len(chunk) {
		offset = len(chunk) - len(pattern)
	}
	copy(chunk[offset:], pattern)

	return chunk
}
