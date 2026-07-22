package security

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const tokenSessionKeyPrefix = "AUTH_SESSION:"

type TokenSessionStore interface {
	Register(ctx context.Context, claims *Claims, idleTimeout time.Duration) error
	Validate(ctx context.Context, claims *Claims) error
	Active(ctx context.Context, claims *Claims) (bool, error)
	Revoke(ctx context.Context, tokenID string) error
}

type RedisTokenSessionStore struct {
	client *redis.Client
}

type tokenSessionRecord struct {
	UserID      int64  `json:"userId"`
	Subject     string `json:"subject"`
	IdleSeconds int64  `json:"idleSeconds"`
}

func NewRedisTokenSessionStore(client *redis.Client) *RedisTokenSessionStore {
	return &RedisTokenSessionStore{client: client}
}

func (s *RedisTokenSessionStore) Register(ctx context.Context, claims *Claims, idleTimeout time.Duration) error {
	if s == nil || s.client == nil || claims == nil || claims.ID == "" {
		return errors.New("token session store is not initialized")
	}
	record := tokenSessionRecord{UserID: claims.UserID, Subject: claims.Subject, IdleSeconds: int64(idleTimeout / time.Second)}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, tokenSessionKeyPrefix+claims.ID, encoded, sessionTTL(claims, idleTimeout)).Err()
}

func (s *RedisTokenSessionStore) Validate(ctx context.Context, claims *Claims) error {
	if s == nil || s.client == nil || claims == nil || claims.ID == "" {
		return errors.New("invalid token session")
	}
	encoded, err := s.client.Get(ctx, tokenSessionKeyPrefix+claims.ID).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return errors.New("token session has expired or been revoked")
		}
		return err
	}
	var record tokenSessionRecord
	if err := json.Unmarshal(encoded, &record); err != nil {
		return err
	}
	if record.UserID != claims.UserID || record.Subject != claims.Subject {
		return errors.New("token session does not match claims")
	}
	idleTimeout := time.Duration(record.IdleSeconds) * time.Second
	ttl := sessionTTL(claims, idleTimeout)
	if ttl > 0 {
		if err := s.client.Expire(ctx, tokenSessionKeyPrefix+claims.ID, ttl).Err(); err != nil {
			return fmt.Errorf("refresh token session: %w", err)
		}
	}
	return nil
}

func (s *RedisTokenSessionStore) Active(ctx context.Context, claims *Claims) (bool, error) {
	if s == nil || s.client == nil || claims == nil || claims.ID == "" {
		return false, nil
	}
	exists, err := s.client.Exists(ctx, tokenSessionKeyPrefix+claims.ID).Result()
	return exists > 0, err
}

func (s *RedisTokenSessionStore) Revoke(ctx context.Context, tokenID string) error {
	if s == nil || s.client == nil || tokenID == "" {
		return nil
	}
	return s.client.Del(ctx, tokenSessionKeyPrefix+tokenID).Err()
}

func sessionTTL(claims *Claims, idleTimeout time.Duration) time.Duration {
	var remaining time.Duration
	if claims != nil && claims.ExpiresAt != nil {
		remaining = time.Until(claims.ExpiresAt.Time)
		if remaining <= 0 {
			return time.Millisecond
		}
	}
	if idleTimeout > 0 && (remaining <= 0 || idleTimeout < remaining) {
		return idleTimeout
	}
	return remaining
}
