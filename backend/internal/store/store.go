package store

import (
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akramjon/cwash/backend/internal/slots"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

var (
	ErrNotFound  = errors.New("not found")
	ErrSlotTaken = errors.New("slot already taken")
)

const (
	StatusBooked    = "booked"
	StatusWaiting   = "waiting"
	StatusWashing   = "washing"
	StatusReady     = "ready"
	StatusDone      = "done"
	StatusCancelled = "cancelled"

	SourceBooking = "booking"
	SourceWalkin  = "walkin"
)

type User struct {
	ID         int64
	TelegramID int64
	Name       string
	Phone      string
}

type Service struct {
	ID          int
	Name        string
	DurationMin int
	Price       int
}

func (s Service) Duration() time.Duration { return time.Duration(s.DurationMin) * time.Minute }

type Order struct {
	ID          int64
	Code        string
	UserID      *int64
	Phone       string
	CarPlate    string
	ServiceID   int
	ServiceName string
	DurationMin int
	Status      string
	Source      string
	ScheduledAt time.Time
	EtaAt       *time.Time
	CreatedAt   time.Time

	ClientTelegramID *int64
	ClientName       *string
}

type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Store struct{ db *pgxpool.Pool }

func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	s := &Store{db: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() { s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	for _, e := range entries { // ReadDir returns sorted names
		var done bool
		if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, e.Name()).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sql, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES ($1)`, e.Name())
			return err
		})
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
	}
	return nil
}

// ---------- users ----------

func (s *Store) UpsertUser(ctx context.Context, tgID int64, name string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(ctx, `
		INSERT INTO users (telegram_id, name) VALUES ($1, $2)
		ON CONFLICT (telegram_id) DO UPDATE SET name = EXCLUDED.name
		RETURNING id, telegram_id, name, phone`, tgID, name).
		Scan(&u.ID, &u.TelegramID, &u.Name, &u.Phone)
	return u, err
}

func (s *Store) SetPhone(ctx context.Context, userID int64, phone string) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET phone=$2 WHERE id=$1`, userID, phone)
	return err
}

// FindUserByPhone matches on the last 9 digits so "901234567" == "998901234567".
func (s *Store) FindUserByPhone(ctx context.Context, phone string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(ctx, `
		SELECT id, telegram_id, name, phone FROM users
		WHERE phone <> '' AND right(phone, 9) = right($1, 9)
		ORDER BY id DESC LIMIT 1`, phone).
		Scan(&u.ID, &u.TelegramID, &u.Name, &u.Phone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// ---------- services ----------

func (s *Store) Services(ctx context.Context) ([]Service, error) {
	rows, err := s.db.Query(ctx, `SELECT id, name, duration_min, price FROM services WHERE active ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Service, error) {
		var sv Service
		err := r.Scan(&sv.ID, &sv.Name, &sv.DurationMin, &sv.Price)
		return sv, err
	})
}

func (s *Store) Service(ctx context.Context, id int) (*Service, error) {
	sv := &Service{}
	err := s.db.QueryRow(ctx, `SELECT id, name, duration_min, price FROM services WHERE id=$1 AND active`, id).
		Scan(&sv.ID, &sv.Name, &sv.DurationMin, &sv.Price)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sv, err
}

// ---------- orders ----------

const orderSelect = `
SELECT o.id, o.code, o.user_id, o.phone, o.car_plate, o.service_id, s.name, s.duration_min,
       o.status, o.source, o.scheduled_at, o.eta_at, o.created_at, u.telegram_id, u.name
FROM orders o
JOIN services s ON s.id = o.service_id
LEFT JOIN users u ON u.id = o.user_id `

func scanOrder(r pgx.Row) (*Order, error) {
	o := &Order{}
	err := r.Scan(&o.ID, &o.Code, &o.UserID, &o.Phone, &o.CarPlate, &o.ServiceID, &o.ServiceName, &o.DurationMin,
		&o.Status, &o.Source, &o.ScheduledAt, &o.EtaAt, &o.CreatedAt, &o.ClientTelegramID, &o.ClientName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return o, err
}

func (s *Store) listOrders(ctx context.Context, where string, args ...any) ([]*Order, error) {
	rows, err := s.db.Query(ctx, orderSelect+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) OrderByID(ctx context.Context, id int64) (*Order, error) {
	return scanOrder(s.db.QueryRow(ctx, orderSelect+`WHERE o.id=$1`, id))
}

// busyIntervals returns time ranges occupied by active orders overlapping [from,to).
// Cars already at the wash (waiting/washing) are stretched to "now + duration"
// or the ETA, whichever is later, so late cars keep blocking their box.
func busyIntervals(ctx context.Context, q querier, from, to time.Time) ([]slots.Interval, error) {
	rows, err := q.Query(ctx, `
		SELECT start_at, end_at FROM (
			SELECT o.scheduled_at AS start_at,
			       GREATEST(
			           o.scheduled_at + make_interval(mins => s.duration_min),
			           COALESCE(o.eta_at, o.scheduled_at),
			           CASE WHEN o.status = 'waiting' THEN now() + make_interval(mins => s.duration_min)
			                ELSE o.scheduled_at END
			       ) AS end_at
			FROM orders o JOIN services s ON s.id = o.service_id
			WHERE o.status IN ('booked','waiting','washing')
		) t
		WHERE start_at < $2 AND end_at > $1`, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (slots.Interval, error) {
		var iv slots.Interval
		err := r.Scan(&iv.Start, &iv.End)
		return iv, err
	})
}

func (s *Store) BusyIntervals(ctx context.Context, from, to time.Time) ([]slots.Interval, error) {
	return busyIntervals(ctx, s.db, from, to)
}

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I confusion

func newCode() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(b)
}

func insertOrder(ctx context.Context, q querier, userID *int64, phone, plate string, serviceID int, status, source string, at time.Time) (int64, error) {
	var id int64
	err := q.QueryRow(ctx, `
		INSERT INTO orders (code, user_id, phone, car_plate, service_id, status, source, scheduled_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		newCode(), userID, phone, plate, serviceID, status, source, at).Scan(&id)
	return id, err
}

// CreateBooking re-checks capacity under a global lock so two clients
// tapping the same slot at once cannot overbook.
func (s *Store) CreateBooking(ctx context.Context, user *User, plate string, svc *Service, start time.Time, boxes int, step time.Duration) (*Order, error) {
	var id int64
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7001)`); err != nil {
			return err
		}
		end := start.Add(svc.Duration())
		busy, err := busyIntervals(ctx, tx, start, end)
		if err != nil {
			return err
		}
		if !slots.Fits(start, end, step, boxes, busy) {
			return ErrSlotTaken
		}
		id, err = insertOrder(ctx, tx, &user.ID, user.Phone, plate, svc.ID, StatusBooked, SourceBooking, start)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.OrderByID(ctx, id)
}

// CreateWalkin registers a car that is physically at the wash right now.
func (s *Store) CreateWalkin(ctx context.Context, userID *int64, phone, plate string, serviceID int) (*Order, error) {
	id, err := insertOrder(ctx, s.db, userID, phone, plate, serviceID, StatusWaiting, SourceWalkin, time.Now())
	if err != nil {
		return nil, err
	}
	return s.OrderByID(ctx, id)
}

// LinkByCode attaches an order to the Telegram user who opened the deep link.
func (s *Store) LinkByCode(ctx context.Context, code string, userID int64) (*Order, error) {
	var id int64
	err := s.db.QueryRow(ctx, `
		UPDATE orders SET user_id=$2, updated_at=now()
		WHERE code=$1 AND (user_id IS NULL OR user_id=$2)
		RETURNING id`, code, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.OrderByID(ctx, id)
}

// LinkByPhone attaches unlinked active walk-ins with this phone to the user.
func (s *Store) LinkByPhone(ctx context.Context, userID int64, phone string) ([]*Order, error) {
	rows, err := s.db.Query(ctx, `
		UPDATE orders SET user_id=$1, updated_at=now()
		WHERE user_id IS NULL AND phone <> '' AND right(phone, 9) = right($2, 9)
		  AND status IN ('booked','waiting','washing','ready')
		RETURNING id`, userID, phone)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return s.listOrders(ctx, `WHERE o.id = ANY($1) ORDER BY o.id`, ids)
}

func (s *Store) ActiveOrdersForUser(ctx context.Context, userID int64) ([]*Order, error) {
	return s.listOrders(ctx, `WHERE o.user_id=$1 AND o.status IN ('booked','waiting','washing','ready')
		ORDER BY o.scheduled_at`, userID)
}

// TodayOrders: cars at the wash plus bookings until end of today.
func (s *Store) TodayOrders(ctx context.Context, dayEnd time.Time) ([]*Order, error) {
	return s.listOrders(ctx, `WHERE o.status IN ('waiting','washing','ready')
		   OR (o.status='booked' AND o.scheduled_at < $1)
		ORDER BY CASE o.status WHEN 'washing' THEN 0 WHEN 'ready' THEN 1 WHEN 'waiting' THEN 2 ELSE 3 END,
		         o.scheduled_at LIMIT 40`, dayEnd)
}

func (s *Store) UpcomingBookings(ctx context.Context, from time.Time) ([]*Order, error) {
	return s.listOrders(ctx, `WHERE o.status='booked' AND o.scheduled_at >= $1
		ORDER BY o.scheduled_at LIMIT 40`, from)
}

// SetStatus moves an order to `to` only if it is currently in one of `from`
// (stale buttons return ErrNotFound). A non-nil eta overwrites eta_at.
func (s *Store) SetStatus(ctx context.Context, id int64, from []string, to string, eta *time.Time) (*Order, error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE orders SET status=$2, eta_at=COALESCE($3, eta_at), updated_at=now()
		WHERE id=$1 AND status = ANY($4)`, id, to, eta, from)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.OrderByID(ctx, id)
}

func (s *Store) ExtendEta(ctx context.Context, id int64, minutes int) (*Order, error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE orders SET eta_at = GREATEST(COALESCE(eta_at, now()), now()) + make_interval(mins => $2),
		                  updated_at = now()
		WHERE id=$1 AND status='washing'`, id, minutes)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.OrderByID(ctx, id)
}

func (s *Store) LastPlates(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT car_plate FROM orders WHERE user_id=$1
		GROUP BY car_plate ORDER BY max(created_at) DESC LIMIT 3`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (s *Store) DueReminders(ctx context.Context, until time.Time) ([]*Order, error) {
	return s.listOrders(ctx, `WHERE o.status='booked' AND o.reminded_at IS NULL
		AND o.scheduled_at > now() AND o.scheduled_at <= $1`, until)
}

func (s *Store) MarkReminded(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `UPDATE orders SET reminded_at=now() WHERE id=$1`, id)
	return err
}
