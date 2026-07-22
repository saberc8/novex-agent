package security

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenService issues JWT tokens compatible with the front-end expectation
// (plain string token returned by /auth/login).
type TokenService struct {
	secret       []byte
	ttl          time.Duration
	sessionStore TokenSessionStore
}

const (
	SubjectAdmin  = "admin"
	SubjectClient = "client"
)

// NewTokenService creates a new TokenService with the given secret and TTL.
func NewTokenService(secret string, ttl time.Duration) *TokenService {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &TokenService{
		secret: []byte(secret),
		ttl:    ttl,
	}
}

// Claims defines minimal JWT claims we care about.
type Claims struct {
	UserID  int64  `json:"userId"`
	Subject string `json:"subType"`
	jwt.RegisteredClaims
}

// Generate issues a token for the given user.
func (s *TokenService) Generate(userID int64) (string, error) {
	return s.GenerateForSubject(userID, SubjectAdmin)
}

// GenerateForSubject issues a token for the given user and auth surface.
func (s *TokenService) GenerateForSubject(userID int64, subject string) (string, error) {
	return s.GenerateForSubjectWithTTL(userID, subject, 0)
}

// GenerateForSubjectWithTTL issues a token using a client-specific lifetime.
// A zero TTL uses the service default; a negative TTL creates a non-expiring
// token whose server-side session can still be revoked.
func (s *TokenService) GenerateForSubjectWithTTL(userID int64, subject string, ttl time.Duration) (string, error) {
	subject = strings.TrimSpace(subject)
	if subject != SubjectAdmin && subject != SubjectClient {
		return "", fmt.Errorf("unsupported token subject %q", subject)
	}
	if userID <= 0 {
		return "", errors.New("user ID must be positive")
	}
	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		return "", fmt.Errorf("generate token ID: %w", err)
	}
	now := time.Now()
	if ttl == 0 {
		ttl = s.ttl
	}
	claims := Claims{
		UserID:  userID,
		Subject: subject,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       hex.EncodeToString(jtiBytes),
			IssuedAt: jwt.NewNumericDate(now),
		},
	}
	if ttl > 0 {
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// Parse extracts Claims from a token string and validates signature and expiry.
func (s *TokenService) Parse(tokenStr string) (*Claims, error) {
	return s.ParseContext(context.Background(), tokenStr)
}

// ParseContext validates signature, expiry, and the server-side session when a
// session store is configured.
func (s *TokenService) ParseContext(ctx context.Context, tokenStr string) (*Claims, error) {
	claims, err := s.parseSignedClaims(tokenStr)
	if err != nil {
		return nil, err
	}
	if s.sessionStore != nil {
		if err := s.sessionStore.Validate(ctx, claims); err != nil {
			return nil, err
		}
	}
	return claims, nil
}

func (s *TokenService) parseSignedClaims(tokenStr string) (*Claims, error) {
	if tokenStr == "" {
		return nil, errors.New("empty token")
	}
	// Allow passing full "Bearer xxx" header value.
	if strings.HasPrefix(strings.ToLower(tokenStr), "bearer ") {
		tokenStr = strings.TrimSpace(tokenStr[7:])
	}
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return s.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// SetSessionStore enables server-side revocation and idle-timeout validation.
// It must be called during application startup before requests are served.
func (s *TokenService) SetSessionStore(store TokenSessionStore) {
	s.sessionStore = store
}

func (s *TokenService) RegisterSession(ctx context.Context, tokenStr string, idleTimeout time.Duration) error {
	if s.sessionStore == nil {
		return nil
	}
	claims, err := s.parseSignedClaims(tokenStr)
	if err != nil {
		return err
	}
	return s.sessionStore.Register(ctx, claims, idleTimeout)
}

func (s *TokenService) RevokeSession(ctx context.Context, tokenStr string) error {
	if s.sessionStore == nil || strings.TrimSpace(tokenStr) == "" {
		return nil
	}
	claims, err := s.parseSignedClaims(tokenStr)
	if err != nil {
		return err
	}
	return s.sessionStore.Revoke(ctx, claims.ID)
}

// IsSessionActive checks signature, expiration, and server-side revocation
// without refreshing the idle timeout.
func (s *TokenService) IsSessionActive(ctx context.Context, tokenStr string) (bool, error) {
	claims, err := s.parseSignedClaims(tokenStr)
	if err != nil {
		return false, nil
	}
	if s.sessionStore == nil {
		return true, nil
	}
	return s.sessionStore.Active(ctx, claims)
}
