package transport

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"jexxor/bytestorm/api"
	"jexxor/bytestorm/core"
	"jexxor/bytestorm/infra"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func TestStreamSearchMetricsSmoke(t *testing.T) {
	svc := core.NewSearchService(core.SIMDEngineID)
	svc.RegisterEngine(core.NewSIMDEngine(0))

	grpcServer := grpc.NewServer()
	api.RegisterSearchServiceServer(grpcServer, NewSearchHandler(svc))

	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen gRPC returned error: %v", err)
	}
	grpcServeErrCh := make(chan error, 1)
	go func() {
		err := grpcServer.Serve(grpcListener)
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			grpcServeErrCh <- err
		}
		close(grpcServeErrCh)
	}()

	metricsListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		grpcServer.Stop()
		_ = grpcListener.Close()
		t.Fatalf("net.Listen metrics returned error: %v", err)
	}

	metricsServer := infra.NewMetricsServer(metricsListener.Addr().String())
	metricsServeErrCh := make(chan error, 1)
	go func() {
		metricsServeErrCh <- metricsServer.Serve(metricsListener)
		close(metricsServeErrCh)
	}()

	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_ = metricsServer.Shutdown(shutdownCtx)
		grpcServer.Stop()
		_ = grpcListener.Close()
		_ = metricsListener.Close()

		if err, ok := <-metricsServeErrCh; ok && err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("metrics server stopped with error: %v", err)
		}
		if err, ok := <-grpcServeErrCh; ok && err != nil {
			t.Errorf("gRPC server stopped with error: %v", err)
		}
	})

	metricsURL := fmt.Sprintf("http://%s/metrics", metricsListener.Addr().String())
	before, err := fetchSIMDTransportMetrics(metricsURL)
	if err != nil {
		t.Fatalf("fetch metrics before stream returned error: %v", err)
	}

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	conn, err := grpc.DialContext(
		dialCtx,
		grpcListener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	dialCancel()
	if err != nil {
		t.Fatalf("grpc.DialContext returned error: %v", err)
	}
	defer conn.Close()

	client := api.NewSearchServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(engineHeader, core.SIMDEngineID))

	stream, err := client.StreamSearch(ctx)
	if err != nil {
		t.Fatalf("StreamSearch returned error: %v", err)
	}

	payload := []byte("xxneedlexxneedle")
	if err := stream.Send(&api.LookupRequest{Pattern: []byte("needle"), Text: payload}); err != nil {
		t.Fatalf("stream.Send returned error: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("stream.CloseSend returned error: %v", err)
	}

	matchCount := 0
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream.Recv returned error: %v", err)
		}
		matchCount++
	}

	if matchCount < 2 {
		t.Fatalf("streamed match count = %d, want >= 2", matchCount)
	}

	after, err := fetchSIMDTransportMetrics(metricsURL)
	if err != nil {
		t.Fatalf("fetch metrics after stream returned error: %v", err)
	}

	if delta := after.processedBytes - before.processedBytes; delta < float64(len(payload)) {
		t.Fatalf("simd_processed_bytes_total delta = %f, want >= %d", delta, len(payload))
	}
	if delta := after.matches - before.matches; delta < 2 {
		t.Fatalf("simd_matches_total delta = %f, want >= 2", delta)
	}
	if delta := after.latencyCount - before.latencyCount; delta < 1 {
		t.Fatalf("simd_processing_latency_seconds_count delta = %f, want >= 1", delta)
	}
}

type simdTransportMetricSnapshot struct {
	processedBytes float64
	matches        float64
	latencyCount   float64
}

func fetchSIMDTransportMetrics(metricsURL string) (simdTransportMetricSnapshot, error) {
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		snapshot, err := fetchSIMDTransportMetricsOnce(metricsURL)
		if err == nil {
			return snapshot, nil
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}

	return simdTransportMetricSnapshot{}, lastErr
}

func fetchSIMDTransportMetricsOnce(metricsURL string) (simdTransportMetricSnapshot, error) {
	resp, err := http.Get(metricsURL)
	if err != nil {
		return simdTransportMetricSnapshot{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return simdTransportMetricSnapshot{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	snapshot := simdTransportMetricSnapshot{}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}

		switch fields[0] {
		case "simd_processed_bytes_total":
			snapshot.processedBytes = value
		case "simd_matches_total":
			snapshot.matches = value
		case "simd_processing_latency_seconds_count":
			snapshot.latencyCount = value
		}
	}

	if err := scanner.Err(); err != nil {
		return simdTransportMetricSnapshot{}, err
	}

	return snapshot, nil
}
