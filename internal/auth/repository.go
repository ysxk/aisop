package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type repository struct {
	pool *pgxpool.Pool
}

type adminRecord struct {
	Admin
	passwordHash string
}

func (r repository) bootstrap(ctx context.Context, username, passwordHash string) error {
	var id uuid.UUID
	return r.pool.QueryRow(ctx, `
		INSERT INTO admins (username, password_hash) VALUES ($1, $2)
		ON CONFLICT DO NOTHING RETURNING id`, username, passwordHash).Scan(&id)
}

func (r repository) cleanup(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= clock_timestamp()`)
	return err
}

func (r repository) begin(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}

func (r repository) loginAdmin(ctx context.Context, tx pgx.Tx, username string) (adminRecord, error) {
	var admin adminRecord
	// SHARE allows concurrent logins but excludes password changes until insertion commits.
	err := tx.QueryRow(ctx, `
		SELECT id, username, created_at, password_hash FROM admins
		WHERE username = $1 FOR SHARE`, username).
		Scan(&admin.ID, &admin.Username, &admin.CreatedAt, &admin.passwordHash)
	return admin, err
}

func (r repository) lockedAdmin(ctx context.Context, tx pgx.Tx, id uuid.UUID) (adminRecord, error) {
	var admin adminRecord
	err := tx.QueryRow(ctx, `
		SELECT id, username, created_at, password_hash FROM admins
		WHERE id = $1 FOR UPDATE`, id).
		Scan(&admin.ID, &admin.Username, &admin.CreatedAt, &admin.passwordHash)
	return admin, err
}

func (r repository) insertSession(ctx context.Context, tx pgx.Tx, hash string, id uuid.UUID, ttl time.Duration) (time.Time, error) {
	var expiresAt time.Time
	err := tx.QueryRow(ctx, `
		INSERT INTO sessions (token_hash, admin_id, expires_at)
		VALUES ($1, $2, clock_timestamp() + $3::interval) RETURNING expires_at`,
		hash, id, pgtype.Interval{Microseconds: ttl.Microseconds(), Valid: true}).Scan(&expiresAt)
	return expiresAt, err
}

func (r repository) session(ctx context.Context, hash string) (Admin, error) {
	var admin Admin
	err := r.pool.QueryRow(ctx, `
		SELECT a.id, a.username, a.created_at FROM sessions s
		JOIN admins a ON a.id = s.admin_id
		WHERE s.token_hash = $1 AND s.expires_at > clock_timestamp()`, hash).
		Scan(&admin.ID, &admin.Username, &admin.CreatedAt)
	return admin, err
}

func (r repository) sessionActive(ctx context.Context, tx pgx.Tx, hash string, id uuid.UUID) (bool, error) {
	var active bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM sessions WHERE token_hash = $1 AND admin_id = $2
		AND expires_at > clock_timestamp())`, hash, id).Scan(&active)
	return active, err
}

func (r repository) logout(ctx context.Context, hash string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hash)
	return err
}

func (r repository) replacePassword(ctx context.Context, tx pgx.Tx, id uuid.UUID, passwordHash string) error {
	if _, err := tx.Exec(ctx, `UPDATE admins SET password_hash = $1 WHERE id = $2`, passwordHash, id); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM sessions WHERE admin_id = $1`, id)
	return err
}
