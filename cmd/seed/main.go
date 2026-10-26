package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/auth"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/store"
)

type demoUser struct {
	Email string
	Name  string
	Role  auth.Role
	Plate *string
}

type demoLot struct {
	Code     string
	Name     string
	Capacity int
}

func main() {
	os.Exit(run(os.Getenv))
}

func run(getenv func(string) string) int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	password := getenv("SEED_DEMO_PASSWORD")
	if password == "" {
		logger.Error("seed: SEED_DEMO_PASSWORD zorunlu")
		return 1
	}
	dbURL := getenv("DATABASE_URL")
	if dbURL == "" {
		logger.Error("seed: DATABASE_URL zorunlu")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		logger.Error("seed: veritabani havuzu olusturulamadi", "hata", err)
		return 1
	}
	defer pool.Close()

	if err := seed(ctx, pool, password); err != nil {
		logger.Error("seed basarisiz", "hata", err)
		return 1
	}
	logger.Info("seed tamam")
	return 0
}

func seed(ctx context.Context, pool *pgxpool.Pool, password string) error {
	if password == "" {
		return errors.New("seed: SEED_DEMO_PASSWORD zorunlu")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("seed: parola ozeti: %w", err)
	}

	for _, u := range demoUsers() {
		if err := store.CreateUserIfMissing(ctx, pool, store.CreateUserParams{
			Email:        u.Email,
			Name:         u.Name,
			Role:         u.Role,
			PasswordHash: hash,
			Plate:        u.Plate,
		}); err != nil {
			return fmt.Errorf("seed: kullanici %s: %w", u.Email, err)
		}
	}

	konum, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		return fmt.Errorf("seed: saat dilimi: %w", err)
	}

	for _, l := range demoLots() {
		if _, err := pool.Exec(ctx,
			`INSERT INTO lots (code, name, capacity) VALUES ($1, $2, $3)
			 ON CONFLICT (code) DO NOTHING`,
			l.Code, l.Name, l.Capacity,
		); err != nil {
			return fmt.Errorf("seed: otopark %s: %w", l.Code, err)
		}
		var lotID int64
		if err := pool.QueryRow(ctx, `SELECT id FROM lots WHERE code = $1`, l.Code).Scan(&lotID); err != nil {
			return fmt.Errorf("seed: otopark %s okunamadi: %w", l.Code, err)
		}

		versionID := "standart-" + strings.ToLower(l.Code)
		if _, err := store.GetTariffVersion(ctx, pool, versionID); err == nil {
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("seed: tarife %s okunamadi: %w", versionID, err)
		}
		if err := store.InsertTariffVersion(ctx, pool, lotID, standartTarife(versionID, konum)); err != nil {
			return fmt.Errorf("seed: tarife %s: %w", versionID, err)
		}
	}

	return nil
}

func demoUsers() []demoUser {
	soforPlaka := "34ABC123"
	return []demoUser{
		{Email: "surucu@parkakil.test", Name: "Surucu Demo", Role: auth.RoleDriver, Plate: &soforPlaka},
		{Email: "gorevli@parkakil.test", Name: "Gorevli Demo", Role: auth.RoleAttendant},
		{Email: "muhasebe@parkakil.test", Name: "Muhasebe Demo", Role: auth.RoleAccountant},
		{Email: "mudur@parkakil.test", Name: "Mudur Demo", Role: auth.RoleManager},
		{Email: "yonetici@parkakil.test", Name: "Yonetici Demo", Role: auth.RoleAdmin},
	}
}

func demoLots() []demoLot {
	return []demoLot{
		{Code: "KADIKOY", Name: "Kadikoy", Capacity: 120},
		{Code: "BESIKTAS", Name: "Besiktas", Capacity: 80},
		{Code: "SISLI", Name: "Sisli", Capacity: 150},
		{Code: "USKUDAR", Name: "Uskudar", Capacity: 60},
		{Code: "BAKIRKOY", Name: "Bakirkoy", Capacity: 100},
	}
}

func standartTarife(id string, konum *time.Location) tariff.Version {
	return tariff.Version{
		ID:          id,
		ValidFrom:   time.Date(2026, 1, 1, 0, 0, 0, 0, konum),
		Location:    konum,
		FreeMinutes: 15,
		Weekday: tariff.DayRule{
			Name: "hafta_ici",
			Bands: []tariff.Band{
				{UpToMinutes: 60, UnitMinutes: 15, UnitPrice: 1000},
				{UnitMinutes: 60, UnitPrice: 3000},
			},
			Night:    &tariff.Night{StartMinute: 1320, EndMinute: 420, UnitMinutes: 60, UnitPrice: 1500},
			DailyCap: 25000,
		},
		Weekend: &tariff.DayRule{
			Name: "hafta_sonu",
			Bands: []tariff.Band{
				{UpToMinutes: 60, UnitMinutes: 15, UnitPrice: 1500},
				{UnitMinutes: 60, UnitPrice: 4000},
			},
			Night:    &tariff.Night{StartMinute: 1320, EndMinute: 420, UnitMinutes: 60, UnitPrice: 2000},
			DailyCap: 30000,
		},
		Events: []tariff.Event{
			{
				Date: tariff.Date{Year: 2026, Month: 11, Day: 1},
				Name: "derbi",
				Rule: tariff.DayRule{
					Name:  "derbi",
					Bands: []tariff.Band{{UnitMinutes: 60, UnitPrice: 10000}},
				},
			},
		},
	}
}
