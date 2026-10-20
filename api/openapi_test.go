package api

import (
	"context"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestOpenAPI(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(OpenAPI)
	if err != nil {
		t.Fatal(err)
	}
	if err = doc.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if op.OperationID == "" || seen[op.OperationID] {
				t.Fatalf("missing/duplicate operationId %s %s", method, path)
			}
			seen[op.OperationID] = true
			if strings.HasPrefix(path, "/api/v1/admin/") && !strings.HasSuffix(path, "/auth/login") && (op.Security == nil || len(*op.Security) == 0) {
				t.Fatalf("admin security missing %s", path)
			}
		}
	}
	if len(seen) != 32 {
		t.Fatalf("expected 32 documented operations, got %d", len(seen))
	}
}
