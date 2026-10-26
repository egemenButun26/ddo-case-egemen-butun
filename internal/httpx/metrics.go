package httpx

import (
	"crypto/sha256"
	"crypto/subtle"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
)

const metricsContentType = "text/plain; version=0.0.4; charset=utf-8"

var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

type counterKey struct {
	method string
	route  string
	status string
}

type histogramKey struct {
	method string
	route  string
}

type histogramData struct {
	buckets []uint64
	sum     float64
	count   uint64
}

type metricsRegistry struct {
	mu         sync.Mutex
	counters   map[counterKey]uint64
	histograms map[histogramKey]*histogramData
	inFlight   int64
	startTime  time.Time
	goVersion  string
}

func newMetricsRegistry() *metricsRegistry {
	return &metricsRegistry{
		counters:   make(map[counterKey]uint64),
		histograms: make(map[histogramKey]*histogramData),
		startTime:  time.Now(),
		goVersion:  runtime.Version(),
	}
}

func (m *metricsRegistry) middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			atomic.AddInt64(&m.inFlight, 1)

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				atomic.AddInt64(&m.inFlight, -1)
				m.observe(r.Method, routePattern(r), sw.status, time.Since(start).Seconds())
			}()

			next.ServeHTTP(sw, r)
		})
	}
}

func routePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return "unmatched"
	}
	if pattern := rctx.RoutePattern(); pattern != "" {
		return pattern
	}
	return "unmatched"
}

func (m *metricsRegistry) observe(method, route string, status int, seconds float64) {
	ck := counterKey{method: method, route: route, status: strconv.Itoa(status)}
	hk := histogramKey{method: method, route: route}

	m.mu.Lock()
	m.counters[ck]++

	h, ok := m.histograms[hk]
	if !ok {
		h = &histogramData{buckets: make([]uint64, len(durationBuckets)+1)}
		m.histograms[hk] = h
	}
	for i, upper := range durationBuckets {
		if seconds <= upper {
			h.buckets[i]++
		}
	}
	h.buckets[len(durationBuckets)]++
	h.sum += seconds
	h.count++
	m.mu.Unlock()
}

func (m *metricsRegistry) snapshot() ([]counterKey, []uint64, []histogramKey, []histogramData, int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	keys := make([]counterKey, 0, len(m.counters))
	for k := range m.counters {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		return keys[i].status < keys[j].status
	})

	values := make([]uint64, len(keys))
	for i, k := range keys {
		values[i] = m.counters[k]
	}

	hkeys := make([]histogramKey, 0, len(m.histograms))
	for k := range m.histograms {
		hkeys = append(hkeys, k)
	}
	sort.Slice(hkeys, func(i, j int) bool {
		if hkeys[i].method != hkeys[j].method {
			return hkeys[i].method < hkeys[j].method
		}
		return hkeys[i].route < hkeys[j].route
	})

	hvalues := make([]histogramData, len(hkeys))
	for i, k := range hkeys {
		h := m.histograms[k]
		buckets := make([]uint64, len(h.buckets))
		copy(buckets, h.buckets)
		hvalues[i] = histogramData{buckets: buckets, sum: h.sum, count: h.count}
	}

	return keys, values, hkeys, hvalues, atomic.LoadInt64(&m.inFlight)
}

func metricsHandler(token string, m *metricsRegistry) http.HandlerFunc {
	expected := sha256.Sum256([]byte(token))
	return func(w http.ResponseWriter, r *http.Request) {
		provided := bearerToken(r.Header.Get("Authorization"))
		actual := sha256.Sum256([]byte(provided))
		if token == "" || provided == "" || subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writePrometheus(w, m)
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

func writePrometheus(w http.ResponseWriter, m *metricsRegistry) {
	counterKeys, counterValues, histogramKeys, histogramValues, inFlight := m.snapshot()

	var b strings.Builder

	b.WriteString("# HELP parkakil_http_requests_total HTTP isteklerinin toplam sayisi.\n")
	b.WriteString("# TYPE parkakil_http_requests_total counter\n")
	for i, k := range counterKeys {
		b.WriteString("parkakil_http_requests_total{method=\"")
		b.WriteString(escapeLabel(k.method))
		b.WriteString("\",route=\"")
		b.WriteString(escapeLabel(k.route))
		b.WriteString("\",status=\"")
		b.WriteString(escapeLabel(k.status))
		b.WriteString("\"} ")
		b.WriteString(strconv.FormatUint(counterValues[i], 10))
		b.WriteString("\n")
	}

	b.WriteString("# HELP parkakil_http_request_duration_seconds HTTP istek suresi saniye cinsinden.\n")
	b.WriteString("# TYPE parkakil_http_request_duration_seconds histogram\n")
	for i, k := range histogramKeys {
		h := histogramValues[i]
		for bi, upper := range durationBuckets {
			b.WriteString("parkakil_http_request_duration_seconds_bucket{method=\"")
			b.WriteString(escapeLabel(k.method))
			b.WriteString("\",route=\"")
			b.WriteString(escapeLabel(k.route))
			b.WriteString("\",le=\"")
			b.WriteString(formatFloat(upper))
			b.WriteString("\"} ")
			b.WriteString(strconv.FormatUint(h.buckets[bi], 10))
			b.WriteString("\n")
		}
		b.WriteString("parkakil_http_request_duration_seconds_bucket{method=\"")
		b.WriteString(escapeLabel(k.method))
		b.WriteString("\",route=\"")
		b.WriteString(escapeLabel(k.route))
		b.WriteString("\",le=\"+Inf\"} ")
		b.WriteString(strconv.FormatUint(h.buckets[len(durationBuckets)], 10))
		b.WriteString("\n")
		b.WriteString("parkakil_http_request_duration_seconds_sum{method=\"")
		b.WriteString(escapeLabel(k.method))
		b.WriteString("\",route=\"")
		b.WriteString(escapeLabel(k.route))
		b.WriteString("\"} ")
		b.WriteString(formatFloat(h.sum))
		b.WriteString("\n")
		b.WriteString("parkakil_http_request_duration_seconds_count{method=\"")
		b.WriteString(escapeLabel(k.method))
		b.WriteString("\",route=\"")
		b.WriteString(escapeLabel(k.route))
		b.WriteString("\"} ")
		b.WriteString(strconv.FormatUint(h.count, 10))
		b.WriteString("\n")
	}

	b.WriteString("# HELP parkakil_http_in_flight_requests Su anda islenen HTTP istek sayisi.\n")
	b.WriteString("# TYPE parkakil_http_in_flight_requests gauge\n")
	b.WriteString("parkakil_http_in_flight_requests ")
	b.WriteString(strconv.FormatInt(inFlight, 10))
	b.WriteString("\n")

	b.WriteString("# HELP parkakil_build_info Servis derleme bilgisi; deger her zaman 1.\n")
	b.WriteString("# TYPE parkakil_build_info gauge\n")
	b.WriteString("parkakil_build_info{go_version=\"")
	b.WriteString(escapeLabel(m.goVersion))
	b.WriteString("\"} 1\n")

	b.WriteString("# HELP process_start_time_seconds Surecin baslangic zamani (Unix saniye).\n")
	b.WriteString("# TYPE process_start_time_seconds gauge\n")
	b.WriteString("process_start_time_seconds ")
	b.WriteString(strconv.FormatFloat(float64(m.startTime.UnixNano())/1e9, 'f', 3, 64))
	b.WriteString("\n")

	w.Header().Set("Content-Type", metricsContentType)
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, b.String())
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return strings.ReplaceAll(value, "\"", "\\\"")
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}
