package media

import (
	"context"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ysxk/aisop/internal/httpapi"
	"github.com/ysxk/aisop/internal/storage"
	_ "golang.org/x/image/webp"
)

type Service struct {
	repo  repository
	files *storage.Local
}

func New(pool *pgxpool.Pool, files *storage.Local) *Service {
	return &Service{repo: repository{pool}, files: files}
}
func unsupported() error {
	return &httpapi.Error{Status: 415, Code: "unsupported_media_type", Message: "Unsupported or malformed media content"}
}
func uploadError(err error) error {
	var maxErr *http.MaxBytesError
	if errors.Is(err, storage.ErrTooLarge) || errors.As(err, &maxErr) {
		return &httpapi.Error{Status: 413, Code: "payload_too_large", Message: "Upload exceeds configured size limit"}
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return httpapi.Invalid("Incomplete multipart upload")
	}
	return err
}
func originalName(name string) (string, error) {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || name == "." || name == "/" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 255 || strings.ContainsFunc(name, unicode.IsControl) {
		return "", httpapi.Invalid("Invalid file name")
	}
	return name, nil
}
func (s *Service) Upload(ctx context.Context, kind string, mr *multipart.Reader, limit int64) (result Media, err error) {
	part, err := mr.NextPart()
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return result, uploadError(err)
		}
		return result, httpapi.Invalid("Expected one multipart file field named file")
	}
	if part.FormName() != "file" || part.FileName() == "" {
		return result, httpapi.Invalid("Expected one multipart file field named file")
	}
	name, err := originalName(part.FileName())
	if err != nil {
		return result, err
	}
	key, size, err := s.files.Save(part, limit)
	defer func() {
		if err != nil && key != "" {
			if cleanupErr := s.files.Remove(key); cleanupErr != nil {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, _ = s.repo.pool.Exec(cleanupCtx, `INSERT INTO file_deletions(storage_key) VALUES ($1) ON CONFLICT DO NOTHING`, key)
			}
		}
	}()
	if err != nil {
		return result, uploadError(err)
	}
	if size == 0 {
		return result, unsupported()
	}
	if _, nextErr := mr.NextPart(); nextErr != io.EOF {
		if nextErr != nil {
			var maxErr *http.MaxBytesError
			if errors.As(nextErr, &maxErr) {
				return result, uploadError(nextErr)
			}
			return result, httpapi.Invalid("Malformed multipart upload")
		}
		return result, httpapi.Invalid("Only one file field is allowed; edit metadata using PATCH")
	}
	mimeType, err := s.inspect(key, kind)
	if err != nil {
		return result, err
	}
	return s.repo.create(ctx, Media{ID: uuid.NewString(), Kind: kind, MIMEType: mimeType, SizeBytes: size, OriginalName: name, DisplayName: name, StorageKey: key})
}
func (s *Service) inspect(key, kind string) (string, error) {
	f, err := s.files.Open(key)
	if err != nil {
		return "", err
	}
	defer f.Close()
	header := make([]byte, 512)
	n, err := io.ReadFull(f, header)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", err
	}
	mimeType := http.DetectContentType(header[:n])
	if kind == "video" {
		if mimeType != "video/mp4" && mimeType != "video/webm" {
			return "", unsupported()
		}
		return mimeType, nil
	}
	if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/gif" && mimeType != "image/webp" {
		return "", unsupported()
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 25000000 {
		return "", unsupported()
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if _, _, err = image.Decode(f); err != nil {
		return "", unsupported()
	}
	return mimeType, nil
}
func (s *Service) Patch(ctx context.Context, id string, p Patch) (Media, error) {
	if p.DisplayName == nil && p.AltText == nil {
		return Media{}, httpapi.Invalid("At least one field is required")
	}
	if p.DisplayName != nil {
		*p.DisplayName = strings.TrimSpace(*p.DisplayName)
		if *p.DisplayName == "" || utf8.RuneCountInString(*p.DisplayName) > 255 || strings.ContainsRune(*p.DisplayName, 0) {
			return Media{}, httpapi.Invalid("display_name must contain 1 to 255 characters without NUL")
		}
	}
	if p.AltText != nil && (utf8.RuneCountInString(*p.AltText) > 1000 || strings.ContainsRune(*p.AltText, 0)) {
		return Media{}, httpapi.Invalid("alt_text must not exceed 1000 characters or contain NUL")
	}
	return s.repo.patch(ctx, id, p)
}

// Deletion intent is committed with the row removal so a failed unlink survives restarts.
func (s *Service) Cleanup(ctx context.Context) error {
	tx, err := s.repo.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT storage_key FROM file_deletions ORDER BY created_at LIMIT 100 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, key)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, key := range keys {
		if err = s.files.Remove(key); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM file_deletions WHERE storage_key=$1`, key); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
