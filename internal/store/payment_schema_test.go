package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	"parkakil/internal/domain/session"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
	"parkakil/migrations"
)

func testZaman(dakika int) time.Time {
	return time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC).Add(time.Duration(dakika) * time.Minute)
}

func testHash() []byte {
	return bytes.Repeat([]byte{0x5a}, 32)
}

func zamanDeger(v *time.Time) any {
	if v == nil {
		return nil
	}
	return *v
}

func metinDeger(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

// oturumHazirlaPlaka yeni lot + tarife + acik oturum olusturur.
func oturumHazirlaPlaka(t *testing.T, pool *pgxpool.Pool, plaka string) (sessionID, lotID int64) {
	t.Helper()
	lotID, tarifeID := lotHazirla(t, pool)
	t0 := testZaman(0)
	giris := olayEkle(t, pool, lotID, session.Entry, t0)
	id, err := store.InsertSession(context.Background(), pool, lotID, plaka, giris, t0, tarifeID)
	if err != nil {
		t.Fatalf("oturum acilamadi: %v", err)
	}
	return id, lotID
}

func kisitHatasi(t *testing.T, err error, kod, kisit string) {
	t.Helper()
	if err == nil {
		t.Fatalf("hata bekleniyordu (%s / %s), nil geldi", kod, kisit)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("pg hatasi bekleniyordu: %v", err)
	}
	if pgErr.Code != kod {
		t.Fatalf("hata kodu = %q, beklenen %q (%v)", pgErr.Code, kod, err)
	}
	if pgErr.ConstraintName != kisit {
		t.Fatalf("kisit = %q, beklenen %q (%v)", pgErr.ConstraintName, kisit, err)
	}
}

func hataKodu(t *testing.T, err error, kod string) {
	t.Helper()
	if err == nil {
		t.Fatalf("hata bekleniyordu (%s), nil geldi", kod)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("pg hatasi bekleniyordu: %v", err)
	}
	if pgErr.Code != kod {
		t.Fatalf("hata kodu = %q, beklenen %q (%v)", pgErr.Code, kod, err)
	}
}

type odemeSatiri struct {
	sessionID    int64
	amount       int64
	status       string
	providerRef  string
	idempotency  string
	requestHash  []byte
	lateApproval bool
	requestedAt  time.Time
	paidAt       *time.Time
	confirmedAt  *time.Time
}

func odemeEkle(t *testing.T, pool *pgxpool.Pool, s odemeSatiri) (int64, error) {
	t.Helper()
	if s.requestHash == nil {
		s.requestHash = testHash()
	}
	if s.requestedAt.IsZero() {
		s.requestedAt = testZaman(1)
	}
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO payments (session_id, amount_kurus, status, provider_ref, idempotency_key,
		                       request_hash, late_approval, requested_at, paid_at, confirmed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		s.sessionID, s.amount, s.status, s.providerRef, s.idempotency, s.requestHash,
		s.lateApproval, s.requestedAt, zamanDeger(s.paidAt), zamanDeger(s.confirmedAt),
	).Scan(&id)
	return id, err
}

func TestPaymentsSemasi(t *testing.T) {
	pool := testdb.Open(t)

	t.Run("gecerli pending ve succeeded", func(t *testing.T) {
		sid, _ := oturumHazirlaPlaka(t, pool, "34PAY001")
		if _, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "pending",
			providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
			requestedAt: testZaman(1),
		}); err != nil {
			t.Fatalf("gecerli pending eklenemedi: %v", err)
		}
		paid := testZaman(2)
		confirmed := testZaman(3)
		if _, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "succeeded",
			providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
			requestedAt: testZaman(1), paidAt: &paid, confirmedAt: &confirmed,
		}); err != nil {
			t.Fatalf("gecerli succeeded eklenemedi: %v", err)
		}
	})

	t.Run("tutar sifir ve negatif", func(t *testing.T) {
		sid, _ := oturumHazirlaPlaka(t, pool, "34PAY002")
		for _, tutar := range []int64{0, -1} {
			_, err := odemeEkle(t, pool, odemeSatiri{
				sessionID: sid, amount: tutar, status: "pending",
				providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
			})
			kisitHatasi(t, err, "23514", "payments_amount_kurus_check")
		}
	})

	t.Run("bilinmeyen status", func(t *testing.T) {
		sid, _ := oturumHazirlaPlaka(t, pool, "34PAY003")
		_, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "baska",
			providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
		})
		kisitHatasi(t, err, "23514", "payments_status_check")
	})

	t.Run("bos provider_ref", func(t *testing.T) {
		sid, _ := oturumHazirlaPlaka(t, pool, "34PAY004")
		_, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "pending",
			providerRef: "", idempotency: benzersiz("idem"),
		})
		kisitHatasi(t, err, "23514", "payments_provider_ref_check")
	})

	t.Run("ayni provider_ref", func(t *testing.T) {
		sid, _ := oturumHazirlaPlaka(t, pool, "34PAY005")
		ref := benzersiz("prov")
		paid := testZaman(2)
		confirmed := testZaman(3)
		if _, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "succeeded",
			providerRef: ref, idempotency: benzersiz("idem"),
			requestedAt: testZaman(1), paidAt: &paid, confirmedAt: &confirmed,
		}); err != nil {
			t.Fatalf("ilk odeme eklenemedi: %v", err)
		}
		_, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "pending",
			providerRef: ref, idempotency: benzersiz("idem"),
		})
		kisitHatasi(t, err, "23505", "payments_provider_ref_uniq")
	})

	t.Run("ayni idempotency_key", func(t *testing.T) {
		sid, _ := oturumHazirlaPlaka(t, pool, "34PAY006")
		key := benzersiz("idem")
		paid := testZaman(2)
		confirmed := testZaman(3)
		if _, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "succeeded",
			providerRef: benzersiz("prov"), idempotency: key,
			requestedAt: testZaman(1), paidAt: &paid, confirmedAt: &confirmed,
		}); err != nil {
			t.Fatalf("ilk odeme eklenemedi: %v", err)
		}
		_, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "pending",
			providerRef: benzersiz("prov"), idempotency: key,
		})
		kisitHatasi(t, err, "23505", "payments_idempotency_key_uniq")
	})

	t.Run("request_hash 31 bayt", func(t *testing.T) {
		sid, _ := oturumHazirlaPlaka(t, pool, "34PAY007")
		_, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "pending",
			providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
			requestHash: bytes.Repeat([]byte{0x11}, 31),
		})
		kisitHatasi(t, err, "23514", "payments_request_hash_check")
	})

	t.Run("idempotency_key uzunlugu", func(t *testing.T) {
		sid, _ := oturumHazirlaPlaka(t, pool, "34PAY012")
		uzun := strings.Repeat("k", 255)
		if _, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "pending",
			providerRef: benzersiz("prov"), idempotency: uzun,
		}); err != nil {
			t.Fatalf("255 karakterlik idempotency_key kabul edilmedi: %v", err)
		}
		sid2, _ := oturumHazirlaPlaka(t, pool, "34PAY013")
		_, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid2, amount: 1000, status: "pending",
			providerRef: benzersiz("prov"), idempotency: uzun + "x",
		})
		hataKodu(t, err, "23514")
	})

	t.Run("succeeded durum tutarliligi", func(t *testing.T) {
		sid, _ := oturumHazirlaPlaka(t, pool, "34PAY008")

		paid := testZaman(2)
		confirmed := testZaman(3)
		_, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "succeeded",
			providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
			requestedAt: testZaman(1), paidAt: nil, confirmedAt: &confirmed,
		})
		kisitHatasi(t, err, "23514", "payments_state_chk")

		erkenPaid := testZaman(0)
		_, err = odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "succeeded",
			providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
			requestedAt: testZaman(1), paidAt: &erkenPaid, confirmedAt: &confirmed,
		})
		kisitHatasi(t, err, "23514", "payments_state_chk")

		erkenConfirmed := testZaman(1)
		_, err = odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "succeeded",
			providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
			requestedAt: testZaman(0), paidAt: &paid, confirmedAt: &erkenConfirmed,
		})
		kisitHatasi(t, err, "23514", "payments_state_chk")
	})

	t.Run("failed paid_at dolu", func(t *testing.T) {
		sid, _ := oturumHazirlaPlaka(t, pool, "34PAY009")
		paid := testZaman(2)
		_, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "failed",
			providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
			paidAt: &paid,
		})
		kisitHatasi(t, err, "23514", "payments_state_chk")
	})

	t.Run("late_approval pending", func(t *testing.T) {
		sid, _ := oturumHazirlaPlaka(t, pool, "34PAY010")
		_, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "pending",
			providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
			lateApproval: true,
		})
		kisitHatasi(t, err, "23514", "payments_late_approval_chk")
	})

	t.Run("oturum basina tek pending", func(t *testing.T) {
		sid, _ := oturumHazirlaPlaka(t, pool, "34PAY011")
		ilkID, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "pending",
			providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
		})
		if err != nil {
			t.Fatalf("ilk pending eklenemedi: %v", err)
		}
		_, err = odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "pending",
			providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
		})
		kisitHatasi(t, err, "23505", "payments_one_pending_per_session")

		if _, err := pool.Exec(context.Background(),
			`UPDATE payments SET status = 'timed_out' WHERE id = $1`, ilkID,
		); err != nil {
			t.Fatalf("ilk odeme timed_out yapilamadi: %v", err)
		}
		if _, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "pending",
			providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
		}); err != nil {
			t.Fatalf("timed_out sonrasi yeni pending eklenemedi: %v", err)
		}

		paid := testZaman(2)
		confirmed := testZaman(3)
		if _, err := pool.Exec(context.Background(),
			`UPDATE payments SET status = 'succeeded', paid_at = $2, confirmed_at = $3
			 WHERE session_id = $1 AND status = 'pending'`,
			sid, paid, confirmed,
		); err != nil {
			t.Fatalf("pending succeeded yapilamadi: %v", err)
		}
		if _, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: sid, amount: 1000, status: "pending",
			providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
		}); err != nil {
			t.Fatalf("succeeded sonrasi yeni pending eklenemedi: %v", err)
		}
	})

	t.Run("olmayan session", func(t *testing.T) {
		_, err := odemeEkle(t, pool, odemeSatiri{
			sessionID: 1 << 60, amount: 1000, status: "pending",
			providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
		})
		kisitHatasi(t, err, "23503", "payments_session_id_fkey")
	})
}

type iadeSatiri struct {
	paymentID   int64
	amount      int64
	status      string
	reason      string
	idempotency string
	requestHash []byte
	providerRef *string
	requestedAt time.Time
	completedAt *time.Time
}

func iadeEkle(t *testing.T, pool *pgxpool.Pool, s iadeSatiri) (int64, error) {
	t.Helper()
	if s.requestHash == nil {
		s.requestHash = testHash()
	}
	if s.requestedAt.IsZero() {
		s.requestedAt = testZaman(1)
	}
	if s.reason == "" {
		s.reason = "overpaid"
	}
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO refunds (payment_id, amount_kurus, status, reason, idempotency_key,
		                      request_hash, provider_ref, requested_at, completed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		s.paymentID, s.amount, s.status, s.reason, s.idempotency, s.requestHash,
		metinDeger(s.providerRef), s.requestedAt, zamanDeger(s.completedAt),
	).Scan(&id)
	return id, err
}

func odemeHazirla(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	sid, _ := oturumHazirlaPlaka(t, pool, "34REF001")
	paid := testZaman(2)
	confirmed := testZaman(3)
	id, err := odemeEkle(t, pool, odemeSatiri{
		sessionID: sid, amount: 5000, status: "succeeded",
		providerRef: benzersiz("prov"), idempotency: benzersiz("idem"),
		requestedAt: testZaman(1), paidAt: &paid, confirmedAt: &confirmed,
	})
	if err != nil {
		t.Fatalf("odeme hazirlanamadi: %v", err)
	}
	return id
}

func TestRefundsSemasi(t *testing.T) {
	pool := testdb.Open(t)

	t.Run("gecerli pending", func(t *testing.T) {
		pid := odemeHazirla(t, pool)
		if _, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 1000, status: "pending",
			idempotency: benzersiz("iade-idem"),
		}); err != nil {
			t.Fatalf("gecerli pending iade eklenemedi: %v", err)
		}
	})

	t.Run("gecerli succeeded ve failed", func(t *testing.T) {
		pid := odemeHazirla(t, pool)
		ref := benzersiz("iade-prov")
		bitis := testZaman(2)
		if _, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 1000, status: "succeeded",
			idempotency: benzersiz("iade-idem"), providerRef: &ref,
			requestedAt: testZaman(1), completedAt: &bitis,
		}); err != nil {
			t.Fatalf("gecerli succeeded iade eklenemedi: %v", err)
		}
		if _, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 1000, status: "failed",
			idempotency: benzersiz("iade-idem"), requestedAt: testZaman(1), completedAt: &bitis,
		}); err != nil {
			t.Fatalf("gecerli failed iade eklenemedi: %v", err)
		}
	})

	t.Run("completed_at requested_at'tan once", func(t *testing.T) {
		pid := odemeHazirla(t, pool)
		ref := benzersiz("iade-prov")
		bitis := testZaman(0)
		_, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 1000, status: "succeeded",
			idempotency: benzersiz("iade-idem"), providerRef: &ref,
			requestedAt: testZaman(1), completedAt: &bitis,
		})
		kisitHatasi(t, err, "23514", "refunds_state_chk")
	})

	t.Run("succeeded completed_at bos", func(t *testing.T) {
		pid := odemeHazirla(t, pool)
		ref := benzersiz("iade-prov")
		_, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 1000, status: "succeeded",
			idempotency: benzersiz("iade-idem"), providerRef: &ref,
			requestedAt: testZaman(1),
		})
		kisitHatasi(t, err, "23514", "refunds_state_chk")
	})

	t.Run("bilinmeyen reason", func(t *testing.T) {
		pid := odemeHazirla(t, pool)
		_, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 1000, status: "pending",
			reason: "baska", idempotency: benzersiz("iade-idem"),
		})
		hataKodu(t, err, "23514")
	})

	t.Run("idempotency_key uzunlugu", func(t *testing.T) {
		pid := odemeHazirla(t, pool)
		uzun := strings.Repeat("k", 255)
		if _, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 1000, status: "pending", idempotency: uzun,
		}); err != nil {
			t.Fatalf("255 karakterlik iade idempotency_key kabul edilmedi: %v", err)
		}
		_, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 1000, status: "pending", idempotency: uzun + "x",
		})
		hataKodu(t, err, "23514")
	})

	t.Run("olmayan payment", func(t *testing.T) {
		_, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: 1 << 60, amount: 1000, status: "pending",
			idempotency: benzersiz("iade-idem"),
		})
		kisitHatasi(t, err, "23503", "refunds_payment_id_fkey")
	})

	t.Run("tutar sifir", func(t *testing.T) {
		pid := odemeHazirla(t, pool)
		_, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 0, status: "pending",
			idempotency: benzersiz("iade-idem"),
		})
		kisitHatasi(t, err, "23514", "refunds_amount_kurus_check")
	})

	t.Run("pending completed_at dolu", func(t *testing.T) {
		pid := odemeHazirla(t, pool)
		bitis := testZaman(2)
		_, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 1000, status: "pending",
			idempotency: benzersiz("iade-idem"), completedAt: &bitis,
		})
		kisitHatasi(t, err, "23514", "refunds_state_chk")
	})

	t.Run("succeeded provider_ref bos", func(t *testing.T) {
		pid := odemeHazirla(t, pool)
		bitis := testZaman(2)
		_, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 1000, status: "succeeded",
			idempotency: benzersiz("iade-idem"), completedAt: &bitis,
		})
		kisitHatasi(t, err, "23514", "refunds_succeeded_ref_chk")
	})

	t.Run("ayni idempotency_key", func(t *testing.T) {
		pid := odemeHazirla(t, pool)
		key := benzersiz("iade-idem")
		if _, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 1000, status: "pending", idempotency: key,
		}); err != nil {
			t.Fatalf("ilk iade eklenemedi: %v", err)
		}
		_, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 1000, status: "pending", idempotency: key,
		})
		kisitHatasi(t, err, "23505", "refunds_idempotency_key_uniq")
	})

	t.Run("iki NULL provider_ref", func(t *testing.T) {
		pid := odemeHazirla(t, pool)
		for i := 0; i < 2; i++ {
			if _, err := iadeEkle(t, pool, iadeSatiri{
				paymentID: pid, amount: 1000, status: "pending",
				idempotency: benzersiz("iade-idem"),
			}); err != nil {
				t.Fatalf("%d. NULL provider_ref iade eklenemedi: %v", i+1, err)
			}
		}
	})

	t.Run("ayni dolu provider_ref", func(t *testing.T) {
		pid := odemeHazirla(t, pool)
		ref := benzersiz("iade-prov")
		if _, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 1000, status: "pending",
			idempotency: benzersiz("iade-idem"), providerRef: &ref,
		}); err != nil {
			t.Fatalf("ilk iade eklenemedi: %v", err)
		}
		_, err := iadeEkle(t, pool, iadeSatiri{
			paymentID: pid, amount: 1000, status: "pending",
			idempotency: benzersiz("iade-idem"), providerRef: &ref,
		})
		kisitHatasi(t, err, "23505", "refunds_provider_ref_uniq")
	})
}

type borcSatiri struct {
	sessionID int64
	lotID     int64
	plate     string
	amount    int64
	paid      int64
	status    string
	closedAt  *time.Time
}

func borcEkle(t *testing.T, pool *pgxpool.Pool, s borcSatiri) (int64, error) {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO debts (session_id, lot_id, plate, amount_kurus, paid_kurus, status, closed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		s.sessionID, s.lotID, s.plate, s.amount, s.paid, s.status, zamanDeger(s.closedAt),
	).Scan(&id)
	return id, err
}

func TestDebtsSemasi(t *testing.T) {
	pool := testdb.Open(t)

	t.Run("gecerli open ve remaining", func(t *testing.T) {
		sid, lotID := oturumHazirlaPlaka(t, pool, "34DEBT01")
		id, err := borcEkle(t, pool, borcSatiri{
			sessionID: sid, lotID: lotID, plate: "34DEBT01",
			amount: 5000, paid: 1000, status: "open",
		})
		if err != nil {
			t.Fatalf("gecerli borc eklenemedi: %v", err)
		}
		var remaining int64
		if err := pool.QueryRow(context.Background(),
			`SELECT remaining_kurus FROM debts WHERE id = $1`, id,
		).Scan(&remaining); err != nil {
			t.Fatalf("remaining_kurus okunamadi: %v", err)
		}
		if remaining != 4000 {
			t.Fatalf("remaining_kurus = %d, beklenen 4000", remaining)
		}
	})

	t.Run("paid amount'tan buyuk", func(t *testing.T) {
		sid, lotID := oturumHazirlaPlaka(t, pool, "34DEBT02")
		_, err := borcEkle(t, pool, borcSatiri{
			sessionID: sid, lotID: lotID, plate: "34DEBT02",
			amount: 1000, paid: 1001, status: "open",
		})
		kisitHatasi(t, err, "23514", "debts_paid_le_amount_chk")
	})

	t.Run("settled eksik odeme", func(t *testing.T) {
		sid, lotID := oturumHazirlaPlaka(t, pool, "34DEBT03")
		bitis := testZaman(5)
		_, err := borcEkle(t, pool, borcSatiri{
			sessionID: sid, lotID: lotID, plate: "34DEBT03",
			amount: 1000, paid: 500, status: "settled", closedAt: &bitis,
		})
		kisitHatasi(t, err, "23514", "debts_state_chk")
	})

	t.Run("open closed_at dolu", func(t *testing.T) {
		sid, lotID := oturumHazirlaPlaka(t, pool, "34DEBT04")
		bitis := testZaman(5)
		_, err := borcEkle(t, pool, borcSatiri{
			sessionID: sid, lotID: lotID, plate: "34DEBT04",
			amount: 1000, paid: 0, status: "open", closedAt: &bitis,
		})
		kisitHatasi(t, err, "23514", "debts_state_chk")
	})

	t.Run("settled closed_at bos", func(t *testing.T) {
		sid, lotID := oturumHazirlaPlaka(t, pool, "34DEBT09")
		_, err := borcEkle(t, pool, borcSatiri{
			sessionID: sid, lotID: lotID, plate: "34DEBT09",
			amount: 1000, paid: 1000, status: "settled",
		})
		kisitHatasi(t, err, "23514", "debts_state_chk")
	})

	t.Run("written_off eksik odeme closed_at bos", func(t *testing.T) {
		sid, lotID := oturumHazirlaPlaka(t, pool, "34DEBT10")
		_, err := borcEkle(t, pool, borcSatiri{
			sessionID: sid, lotID: lotID, plate: "34DEBT10",
			amount: 1000, paid: 400, status: "written_off",
		})
		kisitHatasi(t, err, "23514", "debts_state_chk")
	})

	t.Run("written_off tamamen odendi", func(t *testing.T) {
		sid, lotID := oturumHazirlaPlaka(t, pool, "34DEBT11")
		bitis := testZaman(5)
		_, err := borcEkle(t, pool, borcSatiri{
			sessionID: sid, lotID: lotID, plate: "34DEBT11",
			amount: 1000, paid: 1000, status: "written_off", closedAt: &bitis,
		})
		kisitHatasi(t, err, "23514", "debts_state_chk")
	})

	t.Run("gecerli settled ve written_off", func(t *testing.T) {
		sid, lotID := oturumHazirlaPlaka(t, pool, "34DEBT12")
		bitis := testZaman(5)
		if _, err := borcEkle(t, pool, borcSatiri{
			sessionID: sid, lotID: lotID, plate: "34DEBT12",
			amount: 1000, paid: 1000, status: "settled", closedAt: &bitis,
		}); err != nil {
			t.Fatalf("gecerli settled eklenemedi: %v", err)
		}
		sid2, lotID2 := oturumHazirlaPlaka(t, pool, "34DEBT13")
		if _, err := borcEkle(t, pool, borcSatiri{
			sessionID: sid2, lotID: lotID2, plate: "34DEBT13",
			amount: 1000, paid: 400, status: "written_off", closedAt: &bitis,
		}); err != nil {
			t.Fatalf("gecerli written_off eklenemedi: %v", err)
		}
	})

	t.Run("negatif paid_kurus", func(t *testing.T) {
		sid, lotID := oturumHazirlaPlaka(t, pool, "34DEBT14")
		_, err := borcEkle(t, pool, borcSatiri{
			sessionID: sid, lotID: lotID, plate: "34DEBT14",
			amount: 1000, paid: -1, status: "open",
		})
		hataKodu(t, err, "23514")
	})

	t.Run("oturumdan farkli plaka", func(t *testing.T) {
		sid, lotID := oturumHazirlaPlaka(t, pool, "34DEBT05")
		_, err := borcEkle(t, pool, borcSatiri{
			sessionID: sid, lotID: lotID, plate: "34DEBT06",
			amount: 1000, paid: 0, status: "open",
		})
		kisitHatasi(t, err, "23503", "debts_session_fk")
	})

	t.Run("oturum basina tek borc", func(t *testing.T) {
		sid, lotID := oturumHazirlaPlaka(t, pool, "34DEBT07")
		if _, err := borcEkle(t, pool, borcSatiri{
			sessionID: sid, lotID: lotID, plate: "34DEBT07",
			amount: 1000, paid: 0, status: "open",
		}); err != nil {
			t.Fatalf("ilk borc eklenemedi: %v", err)
		}
		_, err := borcEkle(t, pool, borcSatiri{
			sessionID: sid, lotID: lotID, plate: "34DEBT07",
			amount: 500, paid: 0, status: "open",
		})
		kisitHatasi(t, err, "23505", "debts_session_uniq")
	})

	t.Run("remaining_kurus dogrudan yazma", func(t *testing.T) {
		sid, lotID := oturumHazirlaPlaka(t, pool, "34DEBT08")
		_, err := pool.Exec(context.Background(),
			`INSERT INTO debts (session_id, lot_id, plate, amount_kurus, paid_kurus, remaining_kurus, status)
			 VALUES ($1, $2, $3, 1000, 0, 1, 'open')`,
			sid, lotID, "34DEBT08",
		)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Fatalf("pg hatasi bekleniyordu: %v", err)
		}
		if pgErr.Code != "428C9" {
			t.Fatalf("hata kodu = %q, beklenen 428C9 (%v)", pgErr.Code, err)
		}
	})
}

func tabloVarMi(t *testing.T, db *sql.DB, ad string) bool {
	t.Helper()
	var varMi bool
	if err := db.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, "public."+ad).Scan(&varMi); err != nil {
		t.Fatalf("%s tablosu kontrol edilemedi: %v", ad, err)
	}
	return varMi
}

func kisitVarMi(t *testing.T, db *sql.DB, ad string) bool {
	t.Helper()
	var sayi int
	if err := db.QueryRow(`SELECT count(*) FROM pg_constraint WHERE conname = $1`, ad).Scan(&sayi); err != nil {
		t.Fatalf("%s kisiti kontrol edilemedi: %v", ad, err)
	}
	return sayi > 0
}

func dizinVarMi(t *testing.T, db *sql.DB, ad string) bool {
	t.Helper()
	var sayi int
	if err := db.QueryRow(
		`SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1`, ad,
	).Scan(&sayi); err != nil {
		t.Fatalf("%s dizini kontrol edilemedi: %v", ad, err)
	}
	return sayi > 0
}

func latestMigrationVersion(t *testing.T) int64 {
	t.Helper()
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("migration dosyalari okunamadi: %v", err)
	}
	var latest int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		ad := entry.Name()
		ayrac := strings.IndexByte(ad, '_')
		if ayrac <= 0 {
			continue
		}
		n, err := strconv.ParseInt(ad[:ayrac], 10, 64)
		if err != nil {
			t.Fatalf("migration adi cozulemedi %q: %v", ad, err)
		}
		if n > latest {
			latest = n
		}
	}
	if latest == 0 {
		t.Fatal("migration dosyasi bulunamadi")
	}
	return latest
}

func TestMigrationOdemeTekrar(t *testing.T) {
	testdb.Open(t)
	url := os.Getenv("TEST_DATABASE_URL")
	latest := latestMigrationVersion(t)

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("baglanti acilamadi: %v", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose ayari: %v", err)
	}

	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("ikinci goose.Up basarisiz: %v", err)
	}
	version, err := goose.GetDBVersion(db)
	if err != nil {
		t.Fatalf("surum okunamadi: %v", err)
	}
	if version != latest {
		t.Fatalf("surum = %d, beklenen %d", version, latest)
	}

	if err := goose.DownTo(db, ".", 3); err != nil {
		t.Fatalf("goose.DownTo(3) basarisiz: %v", err)
	}
	for _, ad := range []string{"payments", "refunds", "debts"} {
		if tabloVarMi(t, db, ad) {
			t.Fatalf("down sonrasi %s tablosu hala var", ad)
		}
	}
	if kisitVarMi(t, db, "sessions_id_lot_plate_uniq") {
		t.Fatal("down sonrasi sessions_id_lot_plate_uniq hala var")
	}

	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("yeniden goose.Up basarisiz: %v", err)
	}
	for _, ad := range []string{"payments", "refunds", "debts"} {
		if !tabloVarMi(t, db, ad) {
			t.Fatalf("up sonrasi %s tablosu yok", ad)
		}
	}
	if !kisitVarMi(t, db, "sessions_id_lot_plate_uniq") {
		t.Fatal("up sonrasi sessions_id_lot_plate_uniq yok")
	}
	if !dizinVarMi(t, db, "payments_one_pending_per_session") || !dizinVarMi(t, db, "debts_open_plate_idx") {
		t.Fatal("up sonrasi yeni dizinlerden biri yok")
	}
	version, err = goose.GetDBVersion(db)
	if err != nil {
		t.Fatalf("surum okunamadi: %v", err)
	}
	if version != latest {
		t.Fatalf("yeniden up sonrasi surum = %d, beklenen %d", version, latest)
	}
}
