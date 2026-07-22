package http

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type clientSessionPolicy struct {
	ActiveTimeout time.Duration
	TokenTTL      time.Duration
}

func loadClientSessionPolicy(ctx context.Context, db *sql.DB, clientID, clientType string) (clientSessionPolicy, error) {
	if db == nil {
		return clientSessionPolicy{}, fmt.Errorf("database is not initialized")
	}
	clientID = strings.TrimSpace(clientID)
	clientType = strings.ToUpper(strings.TrimSpace(clientType))
	if clientID == "" || clientType == "" {
		return clientSessionPolicy{}, sql.ErrNoRows
	}
	const query = `
SELECT active_timeout, timeout
FROM sys_client
WHERE client_id = $1
  AND client_type = $2
  AND status = 1
  AND auth_type::text ILIKE '%ACCOUNT%'
LIMIT 1;
`
	var activeSeconds, timeoutSeconds int64
	if err := db.QueryRowContext(ctx, query, clientID, clientType).Scan(&activeSeconds, &timeoutSeconds); err != nil {
		return clientSessionPolicy{}, err
	}
	activeTimeout, err := configuredSeconds(activeSeconds, 30*time.Minute)
	if err != nil {
		return clientSessionPolicy{}, err
	}
	tokenTTL, err := configuredSeconds(timeoutSeconds, 24*time.Hour)
	if err != nil {
		return clientSessionPolicy{}, err
	}
	return clientSessionPolicy{ActiveTimeout: activeTimeout, TokenTTL: tokenTTL}, nil
}

func configuredSeconds(seconds int64, fallback time.Duration) (time.Duration, error) {
	if seconds == -1 {
		return -1, nil
	}
	if seconds == 0 {
		return fallback, nil
	}
	const maxSeconds = int64((10 * 365 * 24 * time.Hour) / time.Second)
	if seconds < -1 || seconds > maxSeconds {
		return 0, fmt.Errorf("configured timeout is outside the supported range")
	}
	return time.Duration(seconds) * time.Second, nil
}
