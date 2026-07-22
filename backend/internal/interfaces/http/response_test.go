package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestFailUsesBusinessCodeAsHTTPStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/forbidden", func(c *gin.Context) {
		Fail(c, "403", "forbidden")
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/forbidden", nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestFailFallsBackToInternalServerErrorForInvalidCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/invalid", func(c *gin.Context) {
		Fail(c, "BUSINESS_ERROR", "failed")
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/invalid", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}
