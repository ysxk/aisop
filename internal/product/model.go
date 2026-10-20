package product

import (
	"time"

	"github.com/ysxk/aisop/internal/httpapi"
)

type Product struct {
	ID          string    `json:"id"`
	SKU         string    `json:"sku"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	PriceMinor  int64     `json:"price_minor"`
	Currency    string    `json:"currency"`
	Stock       int64     `json:"stock"`
	Status      string    `json:"status"`
	CategoryID  string    `json:"category_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Media       []Media   `json:"media"`
}

type Media struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	MIMEType     string    `json:"mime_type"`
	SizeBytes    int64     `json:"size_bytes"`
	OriginalName string    `json:"original_name"`
	DisplayName  string    `json:"display_name"`
	AltText      string    `json:"alt_text"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Position     int       `json:"position"`
	IsCover      bool      `json:"is_cover"`
	ContentURL   string    `json:"content_url"`
}

type productInput struct {
	SKU         *string `json:"sku"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	PriceMinor  *int64  `json:"price_minor"`
	Currency    *string `json:"currency"`
	Stock       *int64  `json:"stock"`
	Status      *string `json:"status"`
	CategoryID  *string `json:"category_id"`
}

type associationInput struct {
	MediaIDs     *[]string `json:"media_ids"`
	CoverMediaID string    `json:"cover_media_id"`
}

type listFilter struct {
	httpapi.Page
	Query      string
	Status     string
	CategoryID *string
}

type listResult struct {
	Items []Product `json:"items"`
	httpapi.Page
}
