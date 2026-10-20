package storage

import (
	"errors"
	"io"
	"os"

	"github.com/google/uuid"
)

var ErrTooLarge = errors.New("file exceeds size limit")

type Local struct{ root *os.Root }

func New(dir string) (*Local, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Local{root: root}, nil
}
func (s *Local) Close() error { return s.root.Close() }
func (s *Local) Save(src io.Reader, limit int64) (key string, size int64, err error) {
	key = uuid.NewString()
	f, err := s.root.OpenFile(key, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return "", 0, err
	}
	defer func() {
		_ = f.Close()
		if err != nil {
			_ = s.root.Remove(key)
		}
	}()
	size, err = io.Copy(f, io.LimitReader(src, limit+1))
	if err != nil {
		return key, size, err
	}
	if size > limit {
		return key, size, ErrTooLarge
	}
	if err = f.Sync(); err != nil {
		return key, size, err
	}
	err = f.Close()
	return key, size, err
}
func (s *Local) Open(key string) (*os.File, error) { return s.root.Open(key) }
func (s *Local) Remove(key string) error {
	err := s.root.Remove(key)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (s *Local) Ready() error {
	name := ".probe-" + uuid.NewString()
	f, err := s.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	closeErr := f.Close()
	removeErr := s.root.Remove(name)
	return errors.Join(closeErr, removeErr)
}
