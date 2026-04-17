package infra

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	simdProcessedBytesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "simd_processed_bytes_total",
		Help: "Total bytes processed by SIMD streaming search.",
	})

	simdMatchesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "simd_matches_total",
		Help: "Total matches produced by SIMD streaming search.",
	})

	simdProcessingLatencySeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "simd_processing_latency_seconds",
		Help:    "Latency of SIMD processing per chunk.",
		Buckets: prometheus.DefBuckets,
	})

	simdEnabledBool = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "simd_enabled_bool",
		Help: "Whether SIMD AVX2 path is enabled (1) or disabled/fallback (0).",
	})
)

func ObserveSIMDChunk(processedBytes int, matches int, latency time.Duration) {
	if processedBytes > 0 {
		simdProcessedBytesTotal.Add(float64(processedBytes))
	}
	if matches > 0 {
		simdMatchesTotal.Add(float64(matches))
	}
	simdProcessingLatencySeconds.Observe(latency.Seconds())
}

func NewMetricsServer(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	return &http.Server{
		Addr:    addr,
		Handler: mux,
	}
}

func FlushMetrics() error {
	_, err := prometheus.DefaultGatherer.Gather()
	return err
}

func SetSIMDEnabled(enabled bool) {
	if enabled {
		simdEnabledBool.Set(1)
		return
	}

	simdEnabledBool.Set(0)
}
