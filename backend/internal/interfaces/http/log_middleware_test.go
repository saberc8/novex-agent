package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"voc-go-backend/internal/domain/syslog"
)

type captureSysLogRepo struct {
	record *syslog.Record
}

func (r *captureSysLogRepo) Save(_ context.Context, record *syslog.Record) error {
	r.record = record
	return nil
}

func TestSysLogMiddlewareRedactsSensitivePayloads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &captureSysLogRepo{}
	router := gin.New()
	router.Use(NewSysLogMiddleware(repo, nil))
	router.POST("/client/auth/login", func(c *gin.Context) {
		OK(c, gin.H{
			"token": "response-token",
			"user":  gin.H{"nickname": "tester"},
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/client/auth/login", strings.NewReader(`{"phone":"13800000000","password":"plain-secret","nested":{"token":"request-token"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer raw-jwt")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if repo.record == nil {
		t.Fatal("expected syslog record")
	}
	for label, value := range map[string]string{
		"request body":    repo.record.RequestBody,
		"response body":   repo.record.ResponseBody,
		"request headers": repo.record.RequestHeaders,
	} {
		for _, sensitive := range []string{"plain-secret", "request-token", "response-token", "raw-jwt"} {
			if strings.Contains(value, sensitive) {
				t.Fatalf("%s leaked sensitive value %q in %s", label, sensitive, value)
			}
		}
	}
	if !strings.Contains(repo.record.RequestBody, `"password":"***"`) {
		t.Fatalf("request body did not redact password: %s", repo.record.RequestBody)
	}
	if !strings.Contains(repo.record.ResponseBody, `"token":"***"`) {
		t.Fatalf("response body did not redact token: %s", repo.record.ResponseBody)
	}
	if !strings.Contains(repo.record.RequestHeaders, `"Authorization":"***"`) {
		t.Fatalf("request headers did not redact Authorization: %s", repo.record.RequestHeaders)
	}
}

func TestSysLogMiddlewareDoesNotLeakSecretsFromTruncatedJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &captureSysLogRepo{}
	router := gin.New()
	router.Use(NewSysLogMiddleware(repo, nil))
	router.POST("/large", func(c *gin.Context) {
		OK(c, true)
	})

	body := `{"password":"must-not-leak","padding":"` + strings.Repeat("x", maxLogBodyLength*2) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/large", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if repo.record == nil {
		t.Fatal("expected syslog record")
	}
	if strings.Contains(repo.record.RequestBody, "must-not-leak") {
		t.Fatalf("truncated JSON leaked a password: %s", repo.record.RequestBody)
	}
	if repo.record.RequestBody != "[json body omitted: invalid or truncated]" {
		t.Fatalf("unexpected request body: %s", repo.record.RequestBody)
	}
}
