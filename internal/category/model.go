package category

import (
	"time"

	"github.com/ysxk/aisop/internal/httpapi"
)

type Category struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	SortOrder   int64     `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type input struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	SortOrder   *int64  `json:"sort_order"`
}

type listResult struct {
	Items []Category `json:"items"`
	httpapi.Page
}
