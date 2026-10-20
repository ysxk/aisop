package product

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestProductWhere(t *testing.T) {
	for _, tt := range []struct {
		name   string
		filter listFilter
		sql    string
		args   []any
	}{
		{"all", listFilter{}, "", []any{}},
		{"status", listFilter{Status: "published"}, " WHERE status = $1", []any{"published"}},
		{"search", listFilter{Query: "hello"}, ` WHERE (sku ILIKE $1 ESCAPE '\' OR name ILIKE $1 ESCAPE '\')`, []any{"%hello%"}},
		{"literal wildcards", listFilter{Status: "draft", Query: `50%_\off`}, ` WHERE status = $1 AND (sku ILIKE $2 ESCAPE '\' OR name ILIKE $2 ESCAPE '\')`, []any{"draft", `%50\%\_\\off%`}},
		{"parameterized", listFilter{Query: `x' OR true --`}, ` WHERE (sku ILIKE $1 ESCAPE '\' OR name ILIKE $1 ESCAPE '\')`, []any{`%x' OR true --%`}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sql, args := productWhere(tt.filter)
			if sql != tt.sql || !reflect.DeepEqual(args, tt.args) {
				t.Fatalf("productWhere = %q, %#v; want %q, %#v", sql, args, tt.sql, tt.args)
			}
		})
	}
}

func TestUUIDArrayEncoding(t *testing.T) {
	ids := []string{"a9c4d18c-65de-4a48-9a40-8f3c3406d877"}
	if _, err := pgtype.NewMap().Encode(pgtype.UUIDArrayOID, pgtype.BinaryFormatCode, uuidArray(ids), nil); err != nil {
		t.Fatal(err)
	}
}

type rollbackTx struct {
	pgx.Tx
	called      bool
	contextErr  error
	hasDeadline bool
}

func (tx *rollbackTx) Rollback(ctx context.Context) error {
	tx.called = true
	tx.contextErr = ctx.Err()
	_, tx.hasDeadline = ctx.Deadline()
	return nil
}

func TestRollbackSurvivesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &rollbackTx{}
	rollback(ctx, tx)
	if !tx.called || tx.contextErr != nil || !tx.hasDeadline {
		t.Fatalf("rollback cleanup context: %+v", tx)
	}
}
