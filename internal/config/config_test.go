package config

import "testing"

func TestLoad(t *testing.T) {
	for _, key := range []string{"ADDR", "MEDIA_DIR", "SESSION_TTL", "IMAGE_MAX_BYTES", "VIDEO_MAX_BYTES", "ALLOWED_ORIGINS", "DATABASE_URL"} {
		t.Setenv(key, "")
	}
	if _, err := Load(); err == nil {
		t.Fatal("missing database URL")
	}
	t.Setenv("DATABASE_URL", "postgres://example/database")
	c, err := Load()
	if err != nil || c.ImageMaxBytes != 10<<20 || c.VideoMaxBytes != 200<<20 || len(c.AllowedOrigins) != 0 {
		t.Fatal(c, err)
	}
	for _, tc := range []struct{ key, value string }{{"SESSION_TTL", "0"}, {"SESSION_TTL", "900h"}, {"IMAGE_MAX_BYTES", "-1"}, {"VIDEO_MAX_BYTES", "999999999999"}, {"ADDR", "invalid"}, {"ALLOWED_ORIGINS", "*"}, {"ALLOWED_ORIGINS", "https://example.com/"}, {"ALLOWED_ORIGINS", "https://user:pass@example.com"}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	t.Setenv("ALLOWED_ORIGINS", "https://shop.example.com,http://localhost:3000")
	if c, err := Load(); err != nil || len(c.AllowedOrigins) != 2 {
		t.Fatal(c, err)
	}
}
