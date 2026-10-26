package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/simulator"
)

const (
	defaultAPIURL       = "http://api:8080"
	defaultSeed         = int64(20261026)
	defaultEventsPerDay = 20000
	defaultSpeed        = 60.0
	defaultStart        = "2026-10-27T06:00:00+03:00"
	alivePath           = "/tmp/simulator-alive"
	aliveFresh          = 60 * time.Second
	simulationDuration  = 24 * time.Hour
	summaryPeriod       = 30 * time.Second
	keepAlivePeriod     = 10 * time.Second
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "dump":
			return runDump(args[1:], stdout, stderr)
		case "healthcheck":
			return runHealthcheck()
		case "send":
			return runSender(getenv, stdout, stderr)
		default:
			fmt.Fprintf(stderr, "bilinmeyen alt komut: %s\n", args[0])
			return 2
		}
	}
	return runSender(getenv, stdout, stderr)
}

type settings struct {
	apiURL       string
	cameraKey    string
	databaseURL  string
	seed         int64
	eventsPerDay int
	speed        float64
	start        time.Time
}

func loadSettings(getenv func(string) string) (settings, error) {
	var s settings
	s.apiURL = strings.TrimRight(envOr(getenv, "SIM_API_URL", defaultAPIURL), "/")
	s.cameraKey = strings.TrimSpace(getenv("CAMERA_API_KEY"))
	if s.cameraKey == "" {
		return s, errors.New("CAMERA_API_KEY zorunlu")
	}
	s.databaseURL = strings.TrimSpace(getenv("DATABASE_URL"))
	if s.databaseURL == "" {
		return s, errors.New("DATABASE_URL zorunlu")
	}

	seed, err := int64Env(getenv, "SIM_SEED", defaultSeed)
	if err != nil {
		return s, err
	}
	s.seed = seed
	events, err := intEnv(getenv, "SIM_EVENTS_PER_DAY", defaultEventsPerDay)
	if err != nil {
		return s, err
	}
	if events <= 0 {
		return s, errors.New("SIM_EVENTS_PER_DAY pozitif olmali")
	}
	s.eventsPerDay = events
	speed, err := floatEnv(getenv, "SIM_SPEED", defaultSpeed)
	if err != nil {
		return s, err
	}
	if speed <= 0 {
		return s, errors.New("SIM_SPEED pozitif olmali")
	}
	s.speed = speed

	s.start = defaultStartTime()
	if deger := strings.TrimSpace(getenv("SIM_START")); deger != "" {
		start, err := time.Parse(time.RFC3339, deger)
		if err != nil {
			return s, fmt.Errorf("SIM_START RFC3339 olmali: %w", err)
		}
		s.start = start
	}
	return s, nil
}

func envOr(getenv func(string) string, anahtar, varsayilan string) string {
	if deger := strings.TrimSpace(getenv(anahtar)); deger != "" {
		return deger
	}
	return varsayilan
}

func int64Env(getenv func(string) string, anahtar string, varsayilan int64) (int64, error) {
	deger := strings.TrimSpace(getenv(anahtar))
	if deger == "" {
		return varsayilan, nil
	}
	sayi, err := strconv.ParseInt(deger, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s sayi olmali: %w", anahtar, err)
	}
	return sayi, nil
}

func intEnv(getenv func(string) string, anahtar string, varsayilan int) (int, error) {
	deger := strings.TrimSpace(getenv(anahtar))
	if deger == "" {
		return varsayilan, nil
	}
	sayi, err := strconv.Atoi(deger)
	if err != nil {
		return 0, fmt.Errorf("%s sayi olmali: %w", anahtar, err)
	}
	return sayi, nil
}

func floatEnv(getenv func(string) string, anahtar string, varsayilan float64) (float64, error) {
	deger := strings.TrimSpace(getenv(anahtar))
	if deger == "" {
		return varsayilan, nil
	}
	sayi, err := strconv.ParseFloat(deger, 64)
	if err != nil {
		return 0, fmt.Errorf("%s sayi olmali: %w", anahtar, err)
	}
	return sayi, nil
}

func defaultStartTime() time.Time {
	start, err := time.Parse(time.RFC3339, defaultStart)
	if err != nil {
		panic(err)
	}
	return start
}

func runSender(getenv func(string) string, stdout, stderr io.Writer) int {
	logger := slog.New(slog.NewJSONHandler(stdout, nil))
	ayar, err := loadSettings(getenv)
	if err != nil {
		logger.Error("simulator ayarlari gecersiz", "hata", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	lots, err := loadLots(ctx, ayar.databaseURL)
	if err != nil {
		logger.Error("otopark listesi okunamadi", "hata", err)
		return 1
	}

	// Kiosk odemeleri ve saglayici gecikmeleri T-4 HTTP uclari geldikten
	// sonra eklenecek; bu adimda uretilmez.
	cfg := simulator.Config{
		Seed:         ayar.seed,
		Lots:         lots,
		Start:        ayar.start,
		Duration:     simulationDuration,
		EventsPerDay: ayar.eventsPerDay,
	}
	events := simulator.Generate(cfg)
	if len(events) == 0 {
		logger.Error("simulator olay uretemedi")
		return 1
	}

	if err := writeAlive(); err != nil {
		logger.Warn("canlilik dosyasi yazilamadi", "hata", err)
	}
	go keepAlive(ctx)
	counter := &counters{}
	go logSummary(ctx, logger, counter)

	gonderici := newSender(ayar.apiURL, ayar.cameraKey, logger, counter)
	logger.Info("simulator basladi",
		"olay", len(events),
		"otopark", len(lots),
		"tohum", ayar.seed,
		"hiz", ayar.speed,
	)

	runStart := time.Now()
	kapanis := false
	for _, ev := range events {
		offset := ev.SendAt.Sub(cfg.Start)
		if offset < 0 {
			offset = 0
		}
		hedef := runStart.Add(time.Duration(float64(offset) / ayar.speed))
		if !waitUntil(ctx, hedef) {
			kapanis = true
			break
		}
		if err := gonderici.send(ctx, ev); err != nil {
			if ctx.Err() != nil {
				kapanis = true
				break
			}
			logger.Warn("olay gonderilemedi",
				"camera_event_id", ev.CameraEventID,
				"hata", err,
			)
		}
	}

	if kapanis {
		logger.Info("kapanis sinyali alindi")
	}
	logSummaryNow(logger, counter)
	logger.Info("simulator kapandi")
	return 0
}

func loadLots(ctx context.Context, databaseURL string) ([]simulator.Lot, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	rows, err := pool.Query(ctx, `SELECT id, code, capacity FROM lots ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lots []simulator.Lot
	for rows.Next() {
		var lot simulator.Lot
		if err := rows.Scan(&lot.ID, &lot.Code, &lot.Capacity); err != nil {
			return nil, err
		}
		lots = append(lots, lot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(lots) == 0 {
		return nil, errors.New("lots tablosu bos")
	}
	return lots, nil
}

func waitUntil(ctx context.Context, hedef time.Time) bool {
	bekleme := time.Until(hedef)
	if bekleme <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(bekleme)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func writeAlive() error {
	return os.WriteFile(alivePath, []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o644)
}

func keepAlive(ctx context.Context) {
	ticker := time.NewTicker(keepAlivePeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = writeAlive()
		}
	}
}

func logSummary(ctx context.Context, logger *slog.Logger, counter *counters) {
	ticker := time.NewTicker(summaryPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logSummaryNow(logger, counter)
		}
	}
}

func logSummaryNow(logger *slog.Logger, counter *counters) {
	logger.Info("simulator ozet",
		"gonderilen", counter.sent.Load(),
		"olusturulan_201", counter.created.Load(),
		"replay_200", counter.replayed.Load(),
		"cakisma_409", counter.conflict.Load(),
		"istemci_4xx", counter.clientError.Load(),
		"hata", counter.failed.Load(),
	)
}

func runHealthcheck() int {
	info, err := os.Stat(alivePath)
	if err != nil {
		return 1
	}
	if time.Since(info.ModTime()) > aliveFresh {
		return 1
	}
	return 0
}

type dumpRecord struct {
	LotID         int64               `json:"lot_id"`
	CameraID      string              `json:"camera_id"`
	CameraEventID string              `json:"camera_event_id"`
	Direction     simulator.Direction `json:"direction"`
	PlateRaw      string              `json:"plate_raw"`
	CapturedAt    time.Time           `json:"captured_at"`
	SendAt        time.Time           `json:"send_at"`
}

func runDump(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dump", flag.ContinueOnError)
	fs.SetOutput(stderr)
	seed := fs.Int64("seed", defaultSeed, "uretim tohumu")
	events := fs.Int("events", 100, "yazilacak olay sayisi")
	out := fs.String("out", "-", "cikis dosyasi (- stdout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *events < 0 {
		fmt.Fprintln(stderr, "events negatif olamaz")
		return 2
	}

	eventsPerDay := *events
	if eventsPerDay < defaultEventsPerDay {
		eventsPerDay = defaultEventsPerDay
	}
	cfg := simulator.Config{
		Seed:         *seed,
		Lots:         fakeLots(),
		Start:        defaultStartTime(),
		Duration:     simulationDuration,
		EventsPerDay: eventsPerDay,
	}
	uretilen := simulator.Generate(cfg)
	if *events < len(uretilen) {
		uretilen = uretilen[:*events]
	}

	var w io.Writer = stdout
	if *out != "-" {
		dosya, err := os.Create(*out)
		if err != nil {
			fmt.Fprintf(stderr, "cikis dosyasi olusturulamadi: %v\n", err)
			return 1
		}
		defer dosya.Close()
		w = dosya
	}

	enc := json.NewEncoder(w)
	for _, ev := range uretilen {
		rec := dumpRecord{
			LotID:         ev.LotID,
			CameraID:      ev.CameraID,
			CameraEventID: ev.CameraEventID,
			Direction:     ev.Direction,
			PlateRaw:      ev.PlateRaw,
			CapturedAt:    ev.CapturedAt,
			SendAt:        ev.SendAt,
		}
		if err := enc.Encode(rec); err != nil {
			fmt.Fprintf(stderr, "olay yazilamadi: %v\n", err)
			return 1
		}
	}
	return 0
}

func fakeLots() []simulator.Lot {
	return []simulator.Lot{
		{ID: 1, Code: "KADIKOY", Capacity: 120},
		{ID: 2, Code: "BESIKTAS", Capacity: 80},
		{ID: 3, Code: "SISLI", Capacity: 150},
		{ID: 4, Code: "USKUDAR", Capacity: 60},
		{ID: 5, Code: "BAKIRKOY", Capacity: 100},
	}
}
