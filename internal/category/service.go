package category

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/ysxk/aisop/internal/httpapi"
)

type service struct{ repo *repository }

func validate(in input, create bool) error {
	if create && in.Name == nil {
		return httpapi.Invalid("name is required")
	}
	if !create && in == (input{}) {
		return httpapi.Invalid("Patch must contain at least one field")
	}
	if in.Name != nil {
		*in.Name = strings.TrimSpace(*in.Name)
		if *in.Name == "" || !utf8.ValidString(*in.Name) || utf8.RuneCountInString(*in.Name) > 100 || strings.ContainsRune(*in.Name, 0) {
			return httpapi.Invalid("name must contain 1 to 100 characters without NUL")
		}
	}
	if in.Description != nil && (!utf8.ValidString(*in.Description) || utf8.RuneCountInString(*in.Description) > 2000 || strings.ContainsRune(*in.Description, 0)) {
		return httpapi.Invalid("description must not exceed 2000 characters or contain NUL")
	}
	if in.SortOrder != nil && *in.SortOrder < 0 {
		return httpapi.Invalid("sort_order must be nonnegative")
	}
	return nil
}
func (s *service) create(ctx context.Context, in input) (Category, error) {
	if err := validate(in, true); err != nil {
		return Category{}, err
	}
	return s.repo.create(ctx, in)
}
func (s *service) patch(ctx context.Context, id string, in input) (Category, error) {
	if err := validate(in, false); err != nil {
		return Category{}, err
	}
	return s.repo.patch(ctx, id, in)
}
