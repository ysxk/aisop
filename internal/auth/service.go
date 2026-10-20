package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ysxk/aisop/internal/httpapi"
	"golang.org/x/crypto/bcrypt"
)

type Admin struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

type loginResponse struct {
	Token     string    `json:"token"`
	TokenType string    `json:"token_type"`
	ExpiresAt time.Time `json:"expires_at"`
	Admin     Admin     `json:"admin"`
}

type identity struct {
	admin     Admin
	tokenHash string
}

type identityKey struct{}

type Service struct {
	repo    repository
	ttl     time.Duration
	limiter *loginLimiter
}

var errInternal = errors.New("Authentication operation failed")

var dummyPasswordHash = func() []byte {
	hash, _ := bcrypt.GenerateFromPassword([]byte("non-authenticating dummy password"), bcrypt.DefaultCost)
	return hash
}()

func New(pool *pgxpool.Pool, ttl time.Duration) *Service {
	return &Service{repo: repository{pool: pool}, ttl: ttl, limiter: newLoginLimiter()}
}

func (s *Service) Bootstrap(ctx context.Context, username, password string) error {
	username, err := validateCredentials(username, password)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return errInternal
	}
	if err := s.repo.bootstrap(ctx, username, string(hash)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Conflict("Administrator already exists")
		}
		return errInternal
	}
	return nil
}

func (s *Service) login(ctx context.Context, username, password string) (loginResponse, error) {
	var response loginResponse
	if err := s.repo.cleanup(ctx); err != nil {
		return response, errInternal
	}
	tx, err := s.repo.begin(ctx)
	if err != nil {
		return response, errInternal
	}
	defer tx.Rollback(ctx)

	admin, err := s.repo.loginAdmin(ctx, tx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
		return response, httpapi.Unauthorized()
	}
	if err != nil {
		return response, errInternal
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.passwordHash), []byte(password)) != nil {
		return response, httpapi.Unauthorized()
	}

	token, hash, err := generateToken()
	if err != nil {
		return response, errInternal
	}
	expiresAt, err := s.repo.insertSession(ctx, tx, hash, admin.ID, s.ttl)
	if err != nil {
		return response, errInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return response, errInternal
	}
	return loginResponse{Token: token, TokenType: "Bearer", ExpiresAt: expiresAt, Admin: admin.Admin}, nil
}

func (s *Service) changePassword(ctx context.Context, who identity, currentPassword, newPassword string) error {
	tx, err := s.repo.begin(ctx)
	if err != nil {
		return errInternal
	}
	defer tx.Rollback(ctx)

	admin, err := s.repo.lockedAdmin(ctx, tx, who.admin.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpapi.Unauthorized()
	}
	if err != nil {
		return errInternal
	}
	// A concurrent password change may have revoked this request while it waited for the lock.
	active, err := s.repo.sessionActive(ctx, tx, who.tokenHash, admin.ID)
	if err != nil {
		return errInternal
	}
	if !active || bcrypt.CompareHashAndPassword([]byte(admin.passwordHash), []byte(currentPassword)) != nil {
		return httpapi.Unauthorized()
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return errInternal
	}
	if err := s.repo.replacePassword(ctx, tx, admin.ID, string(hash)); err != nil {
		return errInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return errInternal
	}
	return nil
}

func validateCredentials(username, password string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" || !utf8.ValidString(username) || utf8.RuneCountInString(username) > 100 {
		return "", httpapi.Invalid("username must contain 1 to 100 characters")
	}
	if err := validatePassword(password); err != nil {
		return "", err
	}
	return username, nil
}

func validatePassword(password string) error {
	if len(password) < 12 || len(password) > 72 {
		return httpapi.Invalid("password must contain 12 to 72 bytes")
	}
	return nil
}

func generateToken() (string, string, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", "", errInternal
	}
	token := base64.RawURLEncoding.EncodeToString(random[:])
	return token, tokenDigest(token), nil
}

func tokenDigest(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func validToken(token string) bool {
	if len(token) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	return err == nil && len(decoded) == 32
}
