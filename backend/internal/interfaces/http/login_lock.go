package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func loginFailureKey(username string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(username))))
	return "AUTH_LOGIN_FAILURE:" + hex.EncodeToString(digest[:])
}

func isLoginLocked(ctx context.Context, client *redis.Client, username string, policy passwordPolicy) (bool, error) {
	if client == nil || policy.LockCount <= 0 || strings.TrimSpace(username) == "" {
		return false, nil
	}
	count, err := client.Get(ctx, loginFailureKey(username)).Int()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return count >= policy.LockCount, nil
}

func recordLoginFailure(ctx context.Context, client *redis.Client, username string, policy passwordPolicy) (bool, error) {
	if client == nil || policy.LockCount <= 0 || strings.TrimSpace(username) == "" {
		return false, nil
	}
	key := loginFailureKey(username)
	count, err := client.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	duration := time.Duration(policy.LockMinutes) * time.Minute
	if err := client.Expire(ctx, key, duration).Err(); err != nil {
		return false, err
	}
	return count >= int64(policy.LockCount), nil
}

func clearLoginFailures(ctx context.Context, client *redis.Client, username string) error {
	if client == nil || strings.TrimSpace(username) == "" {
		return nil
	}
	return client.Del(ctx, loginFailureKey(username)).Err()
}
