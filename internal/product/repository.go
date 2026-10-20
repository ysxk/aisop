package product

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ysxk/aisop/internal/httpapi"
)

type repository struct{ pool *pgxpool.Pool }

const productColumns = `id, sku, name, description, price_minor, currency, stock, status, created_at, updated_at`

func uuidArray(ids []string) []pgtype.UUID {
	values := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		values[i] = pgtype.UUID{Bytes: uuid.MustParse(id), Valid: true}
	}
	return values
}

func scanProduct(row pgx.Row) (Product, error) {
	p := Product{Media: make([]Media, 0)}
	err := row.Scan(&p.ID, &p.SKU, &p.Name, &p.Description, &p.PriceMinor, &p.Currency, &p.Stock, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func rollback(ctx context.Context, tx pgx.Tx) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(cleanup)
}

func productWhere(filter listFilter) (string, []any) {
	clauses := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if filter.Status != "" {
		args = append(args, filter.Status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if filter.Query != "" {
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(filter.Query)
		args = append(args, "%"+escaped+"%")
		n := len(args)
		clauses = append(clauses, fmt.Sprintf(`(sku ILIKE $%d ESCAPE '\' OR name ILIKE $%d ESCAPE '\')`, n, n))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func loadMedia(ctx context.Context, tx pgx.Tx, products []Product, admin bool) error {
	if len(products) == 0 {
		return nil
	}
	ids := make([]string, len(products))
	indexes := make(map[string]int, len(products))
	for i := range products {
		ids[i] = products[i].ID
		indexes[ids[i]] = i
	}
	rows, err := tx.Query(ctx, `SELECT pm.product_id::text, m.id::text, m.kind, m.mime_type, m.size_bytes,
		m.original_name, m.display_name, m.alt_text, m.created_at, m.updated_at, pm.position, pm.is_cover
		FROM product_media pm JOIN media m ON m.id = pm.media_id
		WHERE pm.product_id = ANY($1::uuid[]) ORDER BY pm.product_id, pm.position`, uuidArray(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	prefix := "/api/v1/media/"
	if admin {
		prefix = "/api/v1/admin/media/"
	}
	for rows.Next() {
		var productID string
		var m Media
		if err := rows.Scan(&productID, &m.ID, &m.Kind, &m.MIMEType, &m.SizeBytes, &m.OriginalName, &m.DisplayName, &m.AltText, &m.CreatedAt, &m.UpdatedAt, &m.Position, &m.IsCover); err != nil {
			return err
		}
		m.ContentURL = prefix + m.ID + "/content"
		i := indexes[productID]
		products[i].Media = append(products[i].Media, m)
	}
	return rows.Err()
}

func (repo *repository) list(ctx context.Context, filter listFilter, admin bool) (listResult, error) {
	result := listResult{Items: make([]Product, 0), Page: filter.Page}
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer rollback(ctx, tx)
	where, args := productWhere(filter)
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM products"+where, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	n := len(args)
	query := "SELECT " + productColumns + " FROM products" + where + fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d", n+1, n+2)
	args = append(args, filter.Limit, filter.Offset())
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			rows.Close()
			return result, err
		}
		result.Items = append(result.Items, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if err := loadMedia(ctx, tx, result.Items, admin); err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	return result, nil
}

func (repo *repository) get(ctx context.Context, id string, admin bool) (Product, error) {
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Product{}, err
	}
	defer rollback(ctx, tx)
	query := "SELECT " + productColumns + " FROM products WHERE id = $1"
	if !admin {
		query += " AND status = 'published'"
	}
	p, err := scanProduct(tx.QueryRow(ctx, query, id))
	if err != nil {
		return Product{}, err
	}
	products := []Product{p}
	if err := loadMedia(ctx, tx, products, admin); err != nil {
		return Product{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Product{}, err
	}
	return products[0], nil
}

func (repo *repository) create(ctx context.Context, in productInput) (Product, error) {
	return scanProduct(repo.pool.QueryRow(ctx, `INSERT INTO products
		(sku, name, description, price_minor, currency, stock, status)
		VALUES ($1, $2, COALESCE($3, ''), $4, COALESCE($5, 'CNY'), COALESCE($6::bigint, 0), COALESCE($7, 'draft'))
		RETURNING `+productColumns, in.SKU, in.Name, in.Description, in.PriceMinor, in.Currency, in.Stock, in.Status))
}

func lockProduct(ctx context.Context, tx pgx.Tx, id string) error {
	var locked string
	return tx.QueryRow(ctx, "SELECT id FROM products WHERE id = $1 FOR UPDATE", id).Scan(&locked)
}

func finishMutation(ctx context.Context, tx pgx.Tx, p Product) (Product, error) {
	products := []Product{p}
	if err := loadMedia(ctx, tx, products, true); err != nil {
		return Product{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Product{}, err
	}
	return products[0], nil
}

func (repo *repository) patch(ctx context.Context, id string, in productInput) (Product, error) {
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return Product{}, err
	}
	defer rollback(ctx, tx)
	if err := lockProduct(ctx, tx, id); err != nil {
		return Product{}, err
	}
	p, err := scanProduct(tx.QueryRow(ctx, `UPDATE products SET
		sku = COALESCE($2, sku), name = COALESCE($3, name), description = COALESCE($4, description),
		price_minor = COALESCE($5, price_minor), currency = COALESCE($6, currency),
		stock = COALESCE($7, stock), status = COALESCE($8, status), updated_at = clock_timestamp()
		WHERE id = $1 RETURNING `+productColumns, id, in.SKU, in.Name, in.Description, in.PriceMinor, in.Currency, in.Stock, in.Status))
	if err != nil {
		return Product{}, err
	}
	return finishMutation(ctx, tx, p)
}

func (repo *repository) delete(ctx context.Context, id string) error {
	tag, err := repo.pool.Exec(ctx, "DELETE FROM products WHERE id = $1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpapi.NotFound()
	}
	return nil
}

func lockMedia(ctx context.Context, tx pgx.Tx, ids []string, cover string) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT id::text, kind FROM media WHERE id = ANY($1::uuid[]) ORDER BY id FOR KEY SHARE`, uuidArray(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id, kind string
		if err := rows.Scan(&id, &kind); err != nil {
			return err
		}
		if id == cover && kind != "image" {
			return httpapi.Invalid("Cover media must be an image")
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != len(ids) {
		return httpapi.Invalid("One or more media_ids do not exist")
	}
	return nil
}

func (repo *repository) associate(ctx context.Context, id string, ids []string, cover string) (Product, error) {
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return Product{}, err
	}
	defer rollback(ctx, tx)
	if err := lockProduct(ctx, tx, id); err != nil {
		return Product{}, err
	}
	if err := lockMedia(ctx, tx, ids, cover); err != nil {
		return Product{}, err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM product_media WHERE product_id = $1", id); err != nil {
		return Product{}, err
	}
	if len(ids) > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO product_media (product_id, media_id, position, is_cover)
			SELECT $1::uuid, media_id, (ordinality - 1)::integer, media_id::text = $3
			FROM unnest($2::uuid[]) WITH ORDINALITY AS selected(media_id, ordinality)`, id, uuidArray(ids), cover); err != nil {
			return Product{}, err
		}
	}
	p, err := scanProduct(tx.QueryRow(ctx, "UPDATE products SET updated_at = clock_timestamp() WHERE id = $1 RETURNING "+productColumns, id))
	if err != nil {
		return Product{}, err
	}
	return finishMutation(ctx, tx, p)
}
