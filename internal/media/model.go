package media

import "time"

type Media struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	MIMEType     string    `json:"mime_type"`
	SizeBytes    int64     `json:"size_bytes"`
	OriginalName string    `json:"original_name"`
	DisplayName  string    `json:"display_name"`
	AltText      string    `json:"alt_text"`
	StorageKey   string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ContentURL   string    `json:"content_url"`
}

func (m *Media) setURL(public bool) {
	prefix := "/api/v1/admin/media/"
	if public {
		prefix = "/api/v1/media/"
	}
	m.ContentURL = prefix + m.ID + "/content"
}

type Patch struct {
	DisplayName *string `json:"display_name"`
	AltText     *string `json:"alt_text"`
}
