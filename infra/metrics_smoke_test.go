package infra

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMetricsEndpointSIMDCountersIncrement(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen returned error: %v", err)
	}

	server := NewMetricsServer(listener.Addr().String())
	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- server.Serve(listener)
	}()

	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_ = server.Shutdown(shutdownCtx)
		if err := <-serveErrCh; err != nil && !errorsIsServerClosed(err) {
			t.Errorf("metrics server stopped with error: %v", err)
		}
	})

	metricsURL := fmt.Sprintf("http://%s/metrics", listener.Addr().String())

	before, err := fetchSIMDMetricSnapshot(metricsURL)
	if err != nil {
		t.Fatalf("fetch metrics before update returned error: %v", err)
	}

	ObserveSIMDChunk(1024, 3, 5*time.Millisecond)
	ObserveSIMDChunk(2048, 4, 7*time.Millisecond)

	after, err := fetchSIMDMetricSnapshot(metricsURL)
	if err != nil {
		t.Fatalf("fetch metrics after update returned error: %v", err)
	}

	if delta := after.processedBytes - before.processedBytes; delta < 3072 {
		t.Fatalf("simd_processed_bytes_total delta = %f, want >= 3072", delta)
	}

	if delta := after.matches - before.matches; delta < 7 {
		t.Fatalf("simd_matches_total delta = %f, want >= 7", delta)
	}

	if delta := after.latencyCount - before.latencyCount; delta < 2 {
		t.Fatalf("simd_processing_latency_seconds_count delta = %f, want >= 2", delta)
	}
}

type simdMetricSnapshot struct {
	processedBytes float64
	matches        float64
	latencyCount   float64
}

func fetchSIMDMetricSnapshot(metricsURL string) (simdMetricSnapshot, error) {
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		snapshot, err := fetchSIMDMetricSnapshotOnce(metricsURL)
		if err == nil {
			return snapshot, nil
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}

	return simdMetricSnapshot{}, lastErr
}

func fetchSIMDMetricSnapshotOnce(metricsURL string) (simdMetricSnapshot, error) {
	resp, err := http.Get(metricsURL)
	if err != nil {
		return simdMetricSnapshot{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return simdMetricSnapshot{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	snapshot := simdMetricSnapshot{}
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
		return simdMetricSnapshot{}, err
	}

	return snapshot, nil
}

func errorsIsServerClosed(err error) bool {
	return err == http.ErrServerClosed
}
