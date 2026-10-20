package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr           string
	DatabaseURL    string
	MediaDir       string
	SessionTTL     time.Duration
	ImageMaxBytes  int64
	VideoMaxBytes  int64
	AllowedOrigins []string
}

func Load() (Config, error) {
	c := Config{Addr: env("ADDR", ":8080"), DatabaseURL: os.Getenv("DATABASE_URL"), MediaDir: env("MEDIA_DIR", "./data/media")}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return c, fmt.Errorf("ADDR must be host:port")
	}
	var err error
	c.SessionTTL, err = time.ParseDuration(env("SESSION_TTL", "24h"))
	if err != nil || c.SessionTTL < time.Minute || c.SessionTTL > 30*24*time.Hour {
		return c, fmt.Errorf("SESSION_TTL must be between 1m and 720h")
	}
	for _, item := range []struct {
		name, fallback string
		target         *int64
	}{{"IMAGE_MAX_BYTES", "10485760", &c.ImageMaxBytes}, {"VIDEO_MAX_BYTES", "209715200", &c.VideoMaxBytes}} {
		*item.target, err = strconv.ParseInt(env(item.name, item.fallback), 10, 64)
		if err != nil || *item.target < 1 || *item.target > 10<<30 {
			return c, fmt.Errorf("%s must be between 1 and 10737418240", item.name)
		}
	}
	if raw := os.Getenv("ALLOWED_ORIGINS"); raw != "" {
		for _, origin := range strings.Split(raw, ",") {
			origin = strings.TrimSpace(origin)
			u, err := url.Parse(origin)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
				return c, fmt.Errorf("ALLOWED_ORIGINS must contain exact http(s) origins without paths")
			}
			c.AllowedOrigins = append(c.AllowedOrigins, origin)
		}
	}
	return c, nil
}
func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
