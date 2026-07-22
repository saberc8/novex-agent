package http

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"voc-go-backend/internal/infrastructure/id"
	"voc-go-backend/internal/infrastructure/security"
)

type passwordPolicy struct {
	LockCount       int
	LockMinutes     int
	RepetitionTimes int
	MinLength       int
	AllowUsername   bool
	RequireSymbols  bool
}

func loadPasswordPolicy(ctx context.Context, db *sql.DB) (passwordPolicy, error) {
	policy := passwordPolicy{
		LockCount:       5,
		LockMinutes:     5,
		RepetitionTimes: 3,
		MinLength:       8,
		AllowUsername:   true,
	}
	if db == nil {
		return policy, nil
	}

	values := map[string]*int{
		"PASSWORD_ERROR_LOCK_COUNT":   &policy.LockCount,
		"PASSWORD_ERROR_LOCK_MINUTES": &policy.LockMinutes,
		"PASSWORD_REPETITION_TIMES":   &policy.RepetitionTimes,
		"PASSWORD_MIN_LENGTH":         &policy.MinLength,
	}
	for code, target := range values {
		value, found, err := loadPasswordOption(ctx, db, code)
		if err != nil {
			return passwordPolicy{}, err
		}
		if !found {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return passwordPolicy{}, fmt.Errorf("invalid password option %s: %w", code, err)
		}
		*target = parsed
	}

	for code, target := range map[string]*bool{
		"PASSWORD_ALLOW_CONTAIN_USERNAME": &policy.AllowUsername,
		"PASSWORD_REQUIRE_SYMBOLS":        &policy.RequireSymbols,
	} {
		value, found, err := loadPasswordOption(ctx, db, code)
		if err != nil {
			return passwordPolicy{}, err
		}
		if found {
			*target = value == "1" || strings.EqualFold(value, "true")
		}
	}

	if policy.LockCount < 0 {
		policy.LockCount = 0
	}
	if policy.LockMinutes <= 0 {
		policy.LockMinutes = 5
	}
	if policy.RepetitionTimes < 0 {
		policy.RepetitionTimes = 0
	}
	if policy.RepetitionTimes > 32 {
		policy.RepetitionTimes = 32
	}
	if policy.MinLength < 8 {
		policy.MinLength = 8
	}
	if policy.MinLength > 32 {
		policy.MinLength = 32
	}
	return policy, nil
}

func loadPasswordOption(ctx context.Context, db *sql.DB, code string) (string, bool, error) {
	var raw sql.NullString
	err := db.QueryRowContext(
		ctx,
		`SELECT COALESCE(value, default_value, '') FROM sys_option WHERE code = $1 LIMIT 1`,
		code,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(raw.String), raw.Valid, nil
}

func validatePassword(raw, username string, policy passwordPolicy) error {
	length := len([]rune(raw))
	if length < policy.MinLength || length > 32 {
		return fmt.Errorf("密码长度应为 %d-32 个字符", policy.MinLength)
	}

	var hasLetter, hasDigit, hasSymbol bool
	for _, char := range raw {
		switch {
		case unicode.IsLetter(char):
			hasLetter = true
		case unicode.IsDigit(char):
			hasDigit = true
		default:
			hasSymbol = true
		}
	}
	if !hasLetter || !hasDigit {
		return errors.New("密码必须同时包含字母和数字")
	}
	if policy.RequireSymbols && !hasSymbol {
		return errors.New("密码必须包含特殊字符")
	}

	username = strings.ToLower(strings.TrimSpace(username))
	if !policy.AllowUsername && username != "" {
		password := strings.ToLower(raw)
		if strings.Contains(password, username) || strings.Contains(password, reverseString(username)) {
			return errors.New("密码不能包含正序或倒序用户名")
		}
	}
	return nil
}

func reverseString(value string) string {
	runes := []rune(value)
	for left, right := 0, len(runes)-1; left < right; left, right = left+1, right-1 {
		runes[left], runes[right] = runes[right], runes[left]
	}
	return string(runes)
}

func passwordRecentlyUsed(
	ctx context.Context,
	db *sql.DB,
	verifier security.PasswordVerifier,
	userID int64,
	raw string,
	currentHash string,
	limit int,
) (bool, error) {
	if limit <= 0 || verifier == nil {
		return false, nil
	}
	if currentHash != "" {
		matched, err := verifier.Verify(raw, currentHash)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	if limit == 1 {
		return false, nil
	}

	rows, err := db.QueryContext(ctx, `
SELECT password
FROM sys_user_password_history
WHERE user_id = $1
ORDER BY create_time DESC, id DESC
LIMIT $2`, userID, limit-1)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			return false, err
		}
		matched, err := verifier.Verify(raw, encoded)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, rows.Err()
}

func updatePasswordWithHistory(
	ctx context.Context,
	db *sql.DB,
	userID int64,
	encodedPassword string,
	currentHash string,
	operatorID int64,
	historyLimit int,
) error {
	now := time.Now()
	if historyLimit <= 0 || currentHash == "" {
		_, err := db.ExecContext(ctx,
			`UPDATE sys_user SET password = $1, pwd_reset_time = $2, update_user = $3, update_time = $4 WHERE id = $5`,
			encodedPassword, now, operatorID, now, userID,
		)
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sys_user_password_history (id, user_id, password, create_time) VALUES ($1, $2, $3, $4)`,
		id.Next(), userID, currentHash, now,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sys_user SET password = $1, pwd_reset_time = $2, update_user = $3, update_time = $4 WHERE id = $5`,
		encodedPassword, now, operatorID, now, userID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM sys_user_password_history
WHERE user_id = $1
  AND id NOT IN (
      SELECT id FROM sys_user_password_history
      WHERE user_id = $1
      ORDER BY create_time DESC, id DESC
      LIMIT $2
  )`, userID, historyLimit); err != nil {
		return err
	}
	return tx.Commit()
}
