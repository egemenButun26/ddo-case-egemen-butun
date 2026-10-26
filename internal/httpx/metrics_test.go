package httpx

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const testMetricsToken = "test-metrik-tokeni-0123456789"

func newMetricsTestRouter(token string) http.Handler {
	return newRouter(Deps{
		Logger:       discardLogger(),
		Pinger:       fakePinger{},
		PingTimeout:  time.Second,
		MetricsToken: token,
	}, nil)
}

func metricsRequest(t *testing.T, h http.Handler, header string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func linesContain(body, line string) bool {
	for _, satir := range strings.Split(body, "\n") {
		if satir == line {
			return true
		}
	}
	return false
}

func TestMetricsRequiresToken(t *testing.T) {
	h := newMetricsTestRouter(testMetricsToken)

	cases := map[string]string{
		"baslik yok":   "",
		"yanlis token": "Bearer yanlis-metrik-tokeni-0000",
		"bos bearer":   "Bearer ",
		"farkli sema":  "Basic " + testMetricsToken,
		"onek token":   "Bearer test-metrik",
	}
	for ad, header := range cases {
		t.Run(ad, func(t *testing.T) {
			rec := metricsRequest(t, h, header)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("durum = %d, beklenen 401", rec.Code)
			}
			assertErrorBody(t, rec, "unauthorized")
		})
	}
}

func TestMetricsFailClosedWithoutConfiguredToken(t *testing.T) {
	h := newMetricsTestRouter("")

	rec := metricsRequest(t, h, "Bearer "+testMetricsToken)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("durum = %d, beklenen 401", rec.Code)
	}
	if rec.Code == http.StatusNotFound {
		t.Fatal("token yapilandirilmamisken 404 degil 401 beklenir")
	}
	assertErrorBody(t, rec, "unauthorized")
}

func TestMetricsOKAndContentType(t *testing.T) {
	h := newMetricsTestRouter(testMetricsToken)

	rec := metricsRequest(t, h, "Bearer "+testMetricsToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("durum = %d, beklenen 200; govde: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != metricsContentType {
		t.Fatalf("Content-Type = %q, beklenen %q", ct, metricsContentType)
	}

	body := rec.Body.String()
	for _, ad := range []string{
		"parkakil_http_requests_total",
		"parkakil_http_request_duration_seconds",
		"parkakil_http_in_flight_requests",
		"parkakil_build_info",
		"process_start_time_seconds",
	} {
		if !strings.Contains(body, "# TYPE "+ad+" ") {
			t.Errorf("%s icin # TYPE satiri yok", ad)
		}
	}
	if !strings.Contains(body, `parkakil_build_info{go_version="`+runtime.Version()+`"} 1`) {
		t.Errorf("build_info go_version satiri beklenen bicimde degil:\n%s", body)
	}
	if strings.Contains(body, testMetricsToken) {
		t.Errorf("cikti token icermemeli:\n%s", body)
	}
}

func TestMetricsCountsAndHistogram(t *testing.T) {
	h := newMetricsTestRouter(testMetricsToken)

	for i := 0; i < 3; i++ {
		rec := doRequest(t, h, http.MethodGet, "/health", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("/health durum = %d, beklenen 200", rec.Code)
		}
	}

	rec := metricsRequest(t, h, "Bearer "+testMetricsToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics durum = %d, beklenen 200", rec.Code)
	}
	body := rec.Body.String()

	if !linesContain(body, `parkakil_http_requests_total{method="GET",route="/health",status="200"} 3`) {
		t.Errorf("istek sayaci yanlis:\n%s", body)
	}
	if !linesContain(body, `parkakil_http_request_duration_seconds_count{method="GET",route="/health"} 3`) {
		t.Errorf("histogram _count yanlis:\n%s", body)
	}
	if !linesContain(body, `parkakil_http_request_duration_seconds_bucket{method="GET",route="/health",le="+Inf"} 3`) {
		t.Errorf("histogram +Inf kovasi yanlis:\n%s", body)
	}
	if !strings.Contains(body, `parkakil_http_request_duration_seconds_sum{method="GET",route="/health"} `) {
		t.Errorf("histogram _sum satiri yok:\n%s", body)
	}
	if !linesContain(body, "parkakil_http_in_flight_requests 1") {
		t.Errorf("in-flight gauge /metrics istegi sirasinda 1 olmali:\n%s", body)
	}

	t.Logf("ornek /metrics cikti (ilk 15 satir):\n%s", firstLines(body, 15))
}

func TestMetricsUnmatchedRoute(t *testing.T) {
	h := newMetricsTestRouter(testMetricsToken)

	rec := doRequest(t, h, http.MethodGet, "/bilinmeyen/yol", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("durum = %d, beklenen 404", rec.Code)
	}

	metrics := metricsRequest(t, h, "Bearer "+testMetricsToken)
	body := metrics.Body.String()

	if !linesContain(body, `parkakil_http_requests_total{method="GET",route="unmatched",status="404"} 1`) {
		t.Errorf("bilinmeyen yol route=\"unmatched\" olarak sayilmali:\n%s", body)
	}
	if strings.Contains(body, "/bilinmeyen/yol") {
		t.Errorf("ham URL etiket olarak yazilmamali:\n%s", body)
	}
}

func TestMetricsConcurrentRequests(t *testing.T) {
	h := newMetricsTestRouter(testMetricsToken)

	const adet = 100
	var wg sync.WaitGroup
	wg.Add(adet)
	for i := 0; i < adet; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("/health durum = %d, beklenen 200", rec.Code)
			}
		}()
	}
	wg.Wait()

	rec := metricsRequest(t, h, "Bearer "+testMetricsToken)
	body := rec.Body.String()

	if !linesContain(body, `parkakil_http_requests_total{method="GET",route="/health",status="200"} 100`) {
		t.Errorf("eszamanli 100 istek tam sayilmadi:\n%s", body)
	}
	if !linesContain(body, `parkakil_http_request_duration_seconds_count{method="GET",route="/health"} 100`) {
		t.Errorf("eszamanli histogram _count 100 degil:\n%s", body)
	}
}

func firstLines(metin string, adet int) string {
	satirlar := strings.Split(metin, "\n")
	if len(satirlar) > adet {
		satirlar = satirlar[:adet]
	}
	return strings.Join(satirlar, "\n")
}
