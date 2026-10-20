package category

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ysxk/aisop/internal/httpapi"
)

type repository struct{ pool *pgxpool.Pool }

const columns = `id::text,name,description,sort_order,created_at,updated_at`

func scan(row pgx.Row) (Category, error) {
	var c Category
	err := row.Scan(&c.ID, &c.Name, &c.Description, &c.SortOrder, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}
func (repo *repository) list(ctx context.Context, page httpapi.Page) (listResult, error) {
	result := listResult{Items: make([]Category, 0), Page: page}
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM categories`).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+columns+` FROM categories ORDER BY sort_order,name,id LIMIT $1 OFFSET $2`, page.Limit, page.Offset())
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, c)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func (repo *repository) get(ctx context.Context, id string) (Category, error) {
	return scan(repo.pool.QueryRow(ctx, `SELECT `+columns+` FROM categories WHERE id=$1`, id))
}
func (repo *repository) create(ctx context.Context, in input) (Category, error) {
	return scan(repo.pool.QueryRow(ctx, `INSERT INTO categories(name,description,sort_order) VALUES ($1,COALESCE($2,''),COALESCE($3::bigint,0)) RETURNING `+columns, in.Name, in.Description, in.SortOrder))
}
func (repo *repository) patch(ctx context.Context, id string, in input) (Category, error) {
	return scan(repo.pool.QueryRow(ctx, `UPDATE categories SET name=COALESCE($2,name),description=COALESCE($3,description),sort_order=COALESCE($4,sort_order),updated_at=clock_timestamp() WHERE id=$1 RETURNING `+columns, id, in.Name, in.Description, in.SortOrder))
}
func (repo *repository) delete(ctx context.Context, id string) error {
	tag, err := repo.pool.Exec(ctx, `DELETE FROM categories WHERE id=$1`, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return httpapi.Conflict("Category is referenced by products; reassign or clear their category first")
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpapi.NotFound()
	}
	return nil
}
