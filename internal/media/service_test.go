package media

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"testing"

	"github.com/ysxk/aisop/internal/storage"
	"github.com/ysxk/aisop/internal/testutil"
)

func pngData(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func multipartData(t *testing.T, name string, data []byte, extra bool) *multipart.Reader {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	p, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	p.Write(data)
	if extra {
		w.WriteField("extra", "bad")
	}
	w.Close()
	return multipart.NewReader(&b, w.Boundary())
}
func TestUploadRollbackAndCleanupQueue(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	dir := t.TempDir()
	files, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	s := New(pool, files)
	if _, err = pool.Exec(ctx, `ALTER TABLE media ADD CONSTRAINT reject_test CHECK(display_name <> 'reject.png')`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Upload(ctx, "image", multipartData(t, "reject.png", pngData(t), false), 1024); err == nil {
		t.Fatal("database failure expected")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("database failure leaked file")
	}
	m, err := s.Upload(ctx, "image", multipartData(t, "good.png", pngData(t), false), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if err = files.Remove(m.StorageKey); err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(dir, m.StorageKey)
	if err = os.Mkdir(keyDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(keyDir, "blocker"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.repo.delete(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Cleanup(ctx); err == nil {
		t.Fatal("nonempty directory removal should fail")
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM file_deletions`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err = s.repo.get(ctx, m.ID, false); err == nil {
		t.Fatal("deleted media still accessible")
	}
	if err = os.Remove(filepath.Join(keyDir, "blocker")); err != nil {
		t.Fatal(err)
	}
	if err = New(pool, files).Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM file_deletions`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if _, err = os.Stat(keyDir); !os.IsNotExist(err) {
		t.Fatal("file not cleaned")
	}
}
func TestRejectedUploadsLeaveNoFiles(t *testing.T) {
	dir := t.TempDir()
	files, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	s := New(nil, files)
	for _, tc := range []struct {
		name  string
		data  []byte
		extra bool
		limit int64
	}{{"fake.png", []byte("<html>bad</html>"), false, 1024}, {"bad.png", pngData(t)[:40], false, 1024}, {"extra.png", pngData(t), true, 1024}, {"big.png", pngData(t), false, 2}, {"empty.png", nil, false, 1024}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Upload(context.Background(), "image", multipartData(t, tc.name, tc.data, tc.extra), tc.limit); err == nil {
				t.Fatal("expected rejection")
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatal("rejected upload leaked file")
			}
		})
	}
}
func TestFilename(t *testing.T) {
	for _, name := range []string{"../../image.png", `..\..\image.png`} {
		got, err := originalName(name)
		if err != nil || got != "image.png" {
			t.Fatal(got, err)
		}
	}
	for _, name := range []string{"", ".", "bad\x00.png"} {
		if _, err := originalName(name); err == nil {
			t.Fatal("invalid filename accepted")
		}
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestInterruptedUploadRetriesFailedRemoval(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	dir := t.TempDir()
	files, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	s := New(pool, files)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "interrupted.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(pngData(t)); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	var blocked string
	source := readerFunc(func(p []byte) (int, error) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return 0, err
		}
		if len(entries) > 0 && blocked == "" {
			blocked = filepath.Join(dir, entries[0].Name())
			if err := os.Remove(blocked); err != nil {
				return 0, err
			}
			if err := os.Mkdir(blocked, 0700); err != nil {
				return 0, err
			}
			if err := os.WriteFile(filepath.Join(blocked, "blocker"), []byte("x"), 0600); err != nil {
				return 0, err
			}
			return 0, io.ErrUnexpectedEOF
		}
		return body.Read(p[:1])
	})
	if _, err = s.Upload(ctx, "image", multipart.NewReader(source, w.Boundary()), 1024); err == nil {
		t.Fatal("expected interrupted upload")
	}
	if blocked == "" {
		t.Fatal("failure was not injected")
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM file_deletions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed upload removal must be queued: count=%d err=%v", count, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM media`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if err = os.Remove(filepath.Join(blocked, "blocker")); err != nil {
		t.Fatal(err)
	}
	if err = s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("cleanup retry leaked file", err)
	}
}
