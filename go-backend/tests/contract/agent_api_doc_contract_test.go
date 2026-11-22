package contract_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"go-backend/internal/auth"
)

func TestAgentAPIDocRoutesExistContract(t *testing.T) {
	docPath := filepath.Join("..", "..", "..", "doc", "guide", "api-for-agent.md")
	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}

	re := regexp.MustCompile(`POST (/api/v1/[a-z0-9/_-]+)`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	if len(matches) < 15 {
		t.Fatalf("expected >= 15 documented routes in %s, found %d", docPath, len(matches))
	}

	secret := "contract-jwt-secret"
	router, _ := setupContractRouter(t, secret)
	admin, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	seen := map[string]bool{}
	for _, m := range matches {
		route := m[1]
		if seen[route] {
			continue
		}
		seen[route] = true
		req := httptest.NewRequest(http.MethodPost, route, bytes.NewBufferString(`{}`))
		req.Header.Set("Authorization", admin)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Errorf("documented route %s returned 404", route)
		}
	}
}
