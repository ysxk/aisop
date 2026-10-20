package media

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ysxk/aisop/internal/httpapi"
)

type repository struct{ pool *pgxpool.Pool }

const columns = `id::text,kind,mime_type,size_bytes,original_name,display_name,alt_text,storage_key,created_at,updated_at`

func scan(row pgx.Row) (Media, error) {
	var m Media
	err := row.Scan(&m.ID, &m.Kind, &m.MIMEType, &m.SizeBytes, &m.OriginalName, &m.DisplayName, &m.AltText, &m.StorageKey, &m.CreatedAt, &m.UpdatedAt)
	m.setURL(false)
	return m, err
}
func (r repository) create(ctx context.Context, m Media) (Media, error) {
	return scan(r.pool.QueryRow(ctx, `INSERT INTO media(id,kind,mime_type,size_bytes,original_name,display_name,alt_text,storage_key) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+columns, m.ID, m.Kind, m.MIMEType, m.SizeBytes, m.OriginalName, m.DisplayName, m.AltText, m.StorageKey))
}
func (r repository) get(ctx context.Context, id string, public bool) (Media, error) {
	m, err := scan(r.pool.QueryRow(ctx, `SELECT `+columns+` FROM media WHERE id=$1 AND (NOT $2 OR EXISTS(SELECT 1 FROM product_media pm JOIN products p ON p.id=pm.product_id WHERE pm.media_id=media.id AND p.status='published'))`, id, public))
	m.setURL(public)
	return m, err
}
func (r repository) list(ctx context.Context, p httpapi.Page, kind string) ([]Media, httpapi.Page, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, p, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM media WHERE ($1='' OR kind=$1)`, kind).Scan(&p.Total); err != nil {
		return nil, p, err
	}
	rows, err := tx.Query(ctx, `SELECT `+columns+` FROM media WHERE ($1='' OR kind=$1) ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, kind, p.Limit, p.Offset())
	if err != nil {
		return nil, p, err
	}
	defer rows.Close()
	items := make([]Media, 0)
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, p, err
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, p, err
	}
	return items, p, tx.Commit(ctx)
}
func (r repository) patch(ctx context.Context, id string, p Patch) (Media, error) {
	return scan(r.pool.QueryRow(ctx, `UPDATE media SET display_name=COALESCE($2,display_name),alt_text=COALESCE($3,alt_text),updated_at=now() WHERE id=$1 RETURNING `+columns, id, p.DisplayName, p.AltText))
}
func (r repository) delete(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var key string
	if err = tx.QueryRow(ctx, `SELECT storage_key FROM media WHERE id=$1 FOR UPDATE`, id).Scan(&key); err != nil {
		return err
	}
	var referenced bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_media WHERE media_id=$1)`, id).Scan(&referenced); err != nil {
		return err
	}
	if referenced {
		return httpapi.Conflict("Media is referenced by a product; detach it first")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO file_deletions(storage_key) VALUES ($1)`, key); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM media WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
