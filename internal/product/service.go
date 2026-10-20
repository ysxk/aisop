package product

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/ysxk/aisop/internal/httpapi"
)

type service struct{ repo *repository }

func validStatus(status string) bool {
	return status == "draft" || status == "published" || status == "archived"
}

func validateProduct(in productInput, create bool) error {
	if create && (in.SKU == nil || in.Name == nil || in.PriceMinor == nil) {
		return httpapi.Invalid("sku, name and price_minor are required")
	}
	if !create && in == (productInput{}) {
		return httpapi.Invalid("Patch must contain at least one field")
	}
	for _, field := range []struct {
		name     string
		value    *string
		max      int
		required bool
	}{
		{"sku", in.SKU, 100, true}, {"name", in.Name, 200, true}, {"description", in.Description, 20000, false},
	} {
		if field.value == nil {
			continue
		}
		if utf8.RuneCountInString(*field.value) > field.max || (field.required && strings.TrimSpace(*field.value) == "") {
			return httpapi.Invalid("Invalid " + field.name)
		}
	}
	if in.PriceMinor != nil && *in.PriceMinor < 0 {
		return httpapi.Invalid("price_minor must be nonnegative")
	}
	if in.Stock != nil && *in.Stock < 0 {
		return httpapi.Invalid("stock must be nonnegative")
	}
	if in.Currency != nil {
		if len(*in.Currency) != 3 {
			return httpapi.Invalid("currency must be three uppercase ASCII letters")
		}
		for _, c := range *in.Currency {
			if c < 'A' || c > 'Z' {
				return httpapi.Invalid("currency must be three uppercase ASCII letters")
			}
		}
	}
	if in.Status != nil && !validStatus(*in.Status) {
		return httpapi.Invalid("Invalid status")
	}
	if in.CategoryID != nil && *in.CategoryID != "" {
		id, err := uuid.Parse(*in.CategoryID)
		if err != nil {
			return httpapi.Invalid("category_id must be a UUID or empty")
		}
		*in.CategoryID = id.String()
	}
	return nil
}

func parseFilter(r *http.Request, admin bool) (listFilter, error) {
	page, err := httpapi.Pagination(r)
	if err != nil {
		return listFilter{}, err
	}
	filter := listFilter{Page: page, Status: "published"}
	if values, ok := r.URL.Query()["q"]; ok {
		if len(values) != 1 || !utf8.ValidString(values[0]) || strings.ContainsRune(values[0], 0) || utf8.RuneCountInString(values[0]) > 200 {
			return filter, httpapi.Invalid("Invalid q")
		}
		filter.Query = values[0]
	}
	if values, ok := r.URL.Query()["category_id"]; ok {
		if len(values) != 1 {
			return filter, httpapi.Invalid("Duplicate category_id")
		}
		categoryID := ""
		if values[0] != "none" {
			id, err := uuid.Parse(values[0])
			if err != nil {
				return filter, httpapi.Invalid("category_id must be a UUID or none")
			}
			categoryID = id.String()
		}
		filter.CategoryID = &categoryID
	}
	if admin {
		filter.Status = ""
		if values, ok := r.URL.Query()["status"]; ok {
			if len(values) != 1 || !validStatus(values[0]) {
				return filter, httpapi.Invalid("Invalid status")
			}
			filter.Status = values[0]
		}
	}
	return filter, nil
}

func normalizeAssociation(in associationInput) ([]string, string, error) {
	if in.MediaIDs == nil {
		return nil, "", httpapi.Invalid("media_ids is required and must be an array")
	}
	if len(*in.MediaIDs) > 100 {
		return nil, "", httpapi.Invalid("media_ids may contain at most 100 entries")
	}
	ids := make([]string, 0, len(*in.MediaIDs))
	seen := make(map[string]bool, len(*in.MediaIDs))
	for _, raw := range *in.MediaIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, "", httpapi.Invalid("media_ids must contain UUIDs")
		}
		normalized := id.String()
		if seen[normalized] {
			return nil, "", httpapi.Invalid("media_ids must not contain duplicates")
		}
		seen[normalized] = true
		ids = append(ids, normalized)
	}
	cover := ""
	if in.CoverMediaID != "" {
		id, err := uuid.Parse(in.CoverMediaID)
		if err != nil {
			return nil, "", httpapi.Invalid("cover_media_id must be a UUID or empty")
		}
		cover = id.String()
		if !seen[cover] {
			return nil, "", httpapi.Invalid("cover_media_id must be in media_ids")
		}
	}
	return ids, cover, nil
}

func (s *service) create(ctx context.Context, in productInput) (Product, error) {
	if err := validateProduct(in, true); err != nil {
		return Product{}, err
	}
	return s.repo.create(ctx, in)
}

func (s *service) patch(ctx context.Context, id string, in productInput) (Product, error) {
	if err := validateProduct(in, false); err != nil {
		return Product{}, err
	}
	return s.repo.patch(ctx, id, in)
}

func (s *service) associate(ctx context.Context, id string, in associationInput) (Product, error) {
	ids, cover, err := normalizeAssociation(in)
	if err != nil {
		return Product{}, err
	}
	return s.repo.associate(ctx, id, ids, cover)
}
