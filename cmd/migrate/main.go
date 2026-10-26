package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"parkakil/internal/migrate"
	"parkakil/migrations"
)

func main() {
	os.Exit(run())
}

func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		logger.Error("migrasyon: DATABASE_URL zorunlu")
		return 1
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		logger.Error("migrasyon: veritabani baglantisi acilamadi")
		return 1
	}
	defer db.Close()

	const attempts = 30
	for i := 1; i <= attempts; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		pingErr := db.PingContext(ctx)
		cancel()

		if pingErr == nil {
			migCtx, migCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			version, err := migrate.Up(migCtx, db, migrations.FS)
			migCancel()
			if err != nil {
				logger.Error("migrasyon basarisiz", "hata", err)
				return 1
			}
			logger.Info("migrasyon tamam", "surum", version)
			return 0
		}

		logger.Warn("migrasyon: veritabani hazir degil", "deneme", i, "toplam", attempts)
		if i < attempts {
			time.Sleep(time.Second)
		}
	}

	logger.Error("migrasyon: veritabani zamaninda hazir olmadi")
	return 1
}
