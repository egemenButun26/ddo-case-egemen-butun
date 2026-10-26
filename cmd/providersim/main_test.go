package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

const testSecret = "test-provider-secret-0123456789"

func testSimulator(t *testing.T) *simulator {
	t.Helper()
	sim, err := newSimulator(config{
		Secret:   testSecret,
		Seed:     4242,
		Timeout:  500 * time.Millisecond,
		MinDelay: 0,
		MaxDelay: 0,
	})
	if err != nil {
		t.Fatalf("simulator: %v", err)
	}
	return sim
}

func TestT4ProviderSimAyniTohumAyniKarar(t *testing.T) {
	ilk := make([]outcome, 40)
	ikinci := make([]outcome, 40)
	a := rand.New(rand.NewSource(4242))
	b := rand.New(rand.NewSource(4242))
	for i := range ilk {
		ilk[i] = decide(a)
		ikinci[i] = decide(b)
	}
	for i := range ilk {
		if ilk[i] != ikinci[i] {
			t.Fatalf("ayni tohum farkli karar uretti: %d %q != %q", i, ilk[i], ikinci[i])
		}
	}
	farkli := rand.New(rand.NewSource(7))
	fark := false
	for i := range ilk {
		if decide(farkli) != ilk[i] {
			fark = true
			break
		}
	}
	if !fark {
		t.Fatal("farkli tohum ayni diziyi uretti")
	}
}

type yakalanan struct {
	body  []byte
	sign  string
	mu    sync.Mutex
	adet  int
	kanal chan struct{}
}

func (y *yakalanan) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	y.mu.Lock()
	y.body = body
	y.sign = r.Header.Get("X-Provider-Signature")
	y.adet++
	y.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	select {
	case y.kanal <- struct{}{}:
	default:
	}
}

func (y *yakalanan) sayi() int {
	y.mu.Lock()
	defer y.mu.Unlock()
	return y.adet
}

func TestT4ProviderSimImzaDogrulanir(t *testing.T) {
	y := &yakalanan{kanal: make(chan struct{}, 2)}
	cb := httptest.NewServer(http.HandlerFunc(y.handler))
	defer cb.Close()

	sim := testSimulator(t)
	sim.outcomeF = func() outcome { return outcomeSuccess }
	srv := httptest.NewServer(sim.handler())
	defer srv.Close()

	postCharge(t, srv.URL, "prv-imza", 1000, cb.URL)

	select {
	case <-y.kanal:
	case <-time.After(5 * time.Second):
		t.Fatal("bildirim gelmedi")
	}

	mac := hmac.New(sha256.New, []byte(testSecret))
	_, _ = mac.Write(y.body)
	beklenen := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if y.sign != beklenen {
		t.Fatalf("imza = %q, beklenen %q", y.sign, beklenen)
	}

	var n notificationBody
	if err := json.Unmarshal(y.body, &n); err != nil {
		t.Fatalf("bildirim govdesi: %v", err)
	}
	if n.ProviderRef != "prv-imza" || n.Outcome != string(outcomeSuccess) || n.AmountKurus != 1000 {
		t.Fatalf("bildirim = %+v", n)
	}
	if n.ProviderAt.IsZero() {
		t.Fatal("provider_at bos")
	}
}

func TestT4ProviderSimCiftBildirim(t *testing.T) {
	y := &yakalanan{kanal: make(chan struct{}, 4)}
	cb := httptest.NewServer(http.HandlerFunc(y.handler))
	defer cb.Close()

	sim := testSimulator(t)
	sim.outcomeF = func() outcome { return outcomeDouble }
	srv := httptest.NewServer(sim.handler())
	defer srv.Close()

	postCharge(t, srv.URL, "prv-cift", 2500, cb.URL)

	son := time.After(5 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-y.kanal:
		case <-son:
			t.Fatalf("ikinci bildirim gelmedi (gelen %d)", y.sayi())
		}
	}
	time.Sleep(150 * time.Millisecond)
	if got := y.sayi(); got != 2 {
		t.Fatalf("bildirim sayisi = %d, beklenen 2", got)
	}

	sim.mu.Lock()
	rec, ok := sim.records["prv-cift"]
	sim.mu.Unlock()
	if !ok {
		t.Fatal("basarili islem rapora yazilmadi")
	}
	if rec.AmountKurus != 2500 {
		t.Fatalf("rapor tutari = %d", rec.AmountKurus)
	}
}

func TestT4ProviderSimGecOnay(t *testing.T) {
	y := &yakalanan{kanal: make(chan struct{}, 2)}
	cb := httptest.NewServer(http.HandlerFunc(y.handler))
	defer cb.Close()

	sim := testSimulator(t)
	sim.cfg.Timeout = 150 * time.Millisecond
	sim.outcomeF = func() outcome { return outcomeLate }
	srv := httptest.NewServer(sim.handler())
	defer srv.Close()

	baslangic := time.Now()
	postCharge(t, srv.URL, "prv-gec", 700, cb.URL)

	select {
	case <-y.kanal:
	case <-time.After(5 * time.Second):
		t.Fatal("gec onay bildirimi gelmedi")
	}
	if gecen := time.Since(baslangic); gecen < sim.cfg.Timeout {
		t.Fatalf("gec onay zaman asimindan once geldi: %v", gecen)
	}

	var n notificationBody
	if err := json.Unmarshal(y.body, &n); err != nil {
		t.Fatalf("bildirim govdesi: %v", err)
	}
	if n.ProviderAt.Before(baslangic.Add(sim.cfg.Timeout)) {
		t.Fatalf("provider_at zaman asimindan once: %v", n.ProviderAt)
	}
}

func TestT4ProviderSimRapor(t *testing.T) {
	y := &yakalanan{kanal: make(chan struct{}, 2)}
	cb := httptest.NewServer(http.HandlerFunc(y.handler))
	defer cb.Close()

	sim := testSimulator(t)
	sim.outcomeF = func() outcome { return outcomeSuccess }
	srv := httptest.NewServer(sim.handler())
	defer srv.Close()

	postCharge(t, srv.URL, "prv-rapor", 900, cb.URL)
	select {
	case <-y.kanal:
	case <-time.After(5 * time.Second):
		t.Fatal("bildirim gelmedi")
	}

	resp, err := http.Get(srv.URL + "/report?day=" + time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		t.Fatalf("rapor istegi: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rapor durum = %d", resp.StatusCode)
	}
	var items []record
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("rapor govdesi: %v", err)
	}
	if len(items) != 1 || items[0].ProviderRef != "prv-rapor" || items[0].AmountKurus != 900 {
		t.Fatalf("rapor = %+v", items)
	}
}

func postCharge(t *testing.T, baseURL, ref string, amount int64, callback string) {
	t.Helper()
	body, err := json.Marshal(chargeRequest{
		ProviderRef: ref,
		AmountKurus: amount,
		CallbackURL: callback,
	})
	if err != nil {
		t.Fatalf("govde: %v", err)
	}
	resp, err := http.Post(baseURL+"/charges", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("tahsilat istegi: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("tahsilat durum = %d", resp.StatusCode)
	}
}
