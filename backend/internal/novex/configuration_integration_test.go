package novex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"voc-go-backend/internal/infrastructure/db"
	"voc-go-backend/internal/infrastructure/security"
)

type integrationAPI struct {
	t           *testing.T
	base, token string
}

func (a integrationAPI) call(method, path string, body any, want int) any {
	a.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, e := json.Marshal(body)
		if e != nil {
			a.t.Fatal(e)
		}
		reader = bytes.NewReader(encoded)
	}
	req, e := http.NewRequest(method, a.base+"/api/novex"+path, reader)
	if e != nil {
		a.t.Fatal(e)
	}
	req.Header.Set("Content-Type", "application/json")
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	res, e := client.Do(req)
	if e != nil {
		a.t.Fatal(e)
	}
	defer res.Body.Close()
	var result struct {
		Data any    `json:"data"`
		Msg  string `json:"msg"`
	}
	if e = json.NewDecoder(res.Body).Decode(&result); e != nil {
		a.t.Fatalf("%s %s invalid JSON: %v", method, path, e)
	}
	if res.StatusCode != want {
		a.t.Fatalf("%s %s: got HTTP %d, want %d (%s)", method, path, res.StatusCode, want, result.Msg)
	}
	return result.Data
}
func (a integrationAPI) login(username, password string) integrationAPI {
	a.t.Helper()
	result := a.call("POST", "/login", map[string]any{"username": username, "password": password}, 200).(map[string]any)
	a.token = result["token"].(string)
	return a
}

// This exercises entirely new metadata through the public HTTP APIs. The fake runner
// below is test-only: it reads the real authorized MCP data before a controlled
// revocation, then attempts to publish it. No production answer is synthesized.
func TestIntegrationDynamicConfigurationAndRevocation(t *testing.T) {
	if os.Getenv("NOVEX_INTEGRATION") != "1" {
		t.Skip("requires running seeded Novex demo")
	}
	apiURL := env("NOVEX_TEST_API_URL", "http://127.0.0.1:8310")
	adminPassword := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if adminPassword == "" {
		t.Fatal("BOOTSTRAP_ADMIN_PASSWORD is required for the opted-in integration test")
	}
	admin := integrationAPI{t: t, base: apiURL}.login("admin", adminPassword)
	pg, e := db.NewPostgres(db.LoadConfigFromEnv())
	if e != nil {
		t.Fatal(e)
	}
	defer pg.Close()
	suffix := strconv.FormatInt(time.Now().UnixMilli(), 36)
	tenant := "integration-internal-" + suffix
	username := "novex_it_" + suffix
	password := "IntegrationOnly2026!"
	var datasourceID, resourceID, grantID string
	var uid, roleID int64
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, stmt := range []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM novex_messages WHERE conversation_id IN (SELECT id FROM novex_conversations WHERE user_id=$1)`, []any{uid}},
			{`DELETE FROM novex_runs WHERE user_id=$1`, []any{uid}},
			{`DELETE FROM novex_conversations WHERE user_id=$1`, []any{uid}},
			{`DELETE FROM novex_embed_tickets WHERE user_id=$1`, []any{uid}},
			{`DELETE FROM novex_grants WHERE resource_id=$1`, []any{resourceID}},
			{`DELETE FROM novex_identities WHERE user_id=$1`, []any{uid}},
			{`DELETE FROM novex_resources WHERE id=$1`, []any{resourceID}},
			{`DELETE FROM novex_datasources WHERE id=$1`, []any{datasourceID}},
			{`DELETE FROM sys_user_role WHERE user_id=$1`, []any{uid}},
			{`DELETE FROM novex_users WHERE user_id=$1`, []any{uid}},
			{`DELETE FROM sys_user WHERE id=$1`, []any{uid}},
			{`DELETE FROM sys_role WHERE id=$1`, []any{roleID}},
			{`DELETE FROM novex_audit WHERE user_id=$1 OR resource_id IN ($2,$3)`, []any{uid, resourceID, datasourceID}},
		} {
			if _, e = pg.ExecContext(ctx, stmt.sql, stmt.args...); e != nil {
				t.Errorf("test cleanup: %v", e)
			}
		}
	}()
	port, _ := strconv.Atoi(env("NOVEX_HR_PORT", "5432"))
	created := admin.call("POST", "/admin/datasources", map[string]any{"name": "Integration source " + suffix, "host": env("NOVEX_HR_HOST", "127.0.0.1"), "port": port, "database": env("NOVEX_HR_DATABASE", "novex-hr"), "username": env("NOVEX_HR_USER", "novex_hr_read"), "password": os.Getenv("NOVEX_HR_PASSWORD"), "sslMode": "disable", "tenantId": tenant}, 200).(map[string]any)
	datasourceID = created["id"].(string)
	if _, exists := created["password"]; exists {
		t.Fatal("datasource response leaked password")
	}
	discovered := admin.call("POST", "/admin/datasources/"+datasourceID+"/introspect", map[string]any{}, 200).(map[string]any)
	if len(discovered["tables"].([]any)) == 0 {
		t.Fatal("schema introspection returned no tables")
	}
	resource := admin.call("POST", "/admin/resources", map[string]any{"name": "Integration payroll " + suffix, "key": "it_payroll_" + suffix, "datasourceId": datasourceID, "schema": "public", "table": "novex_demo_payroll", "tenantColumn": "tenant_id", "ownerColumn": "employee_id", "departmentColumn": "department_id", "identityRequired": true, "columns": []string{"employee_id", "total", "department_id"}}, 200).(map[string]any)
	resourceID = resource["id"].(string)
	role := admin.call("POST", "/admin/roles", map[string]any{"name": "Integration " + suffix, "code": "it_" + suffix}, 200).(map[string]any)
	roleID = int64(role["id"].(float64))
	createdUser := admin.call("POST", "/admin/users", map[string]any{"username": username, "nickname": "Integration Employee", "password": password, "tenantId": tenant, "departmentId": "internal-support", "roleIds": []int64{roleID}}, 200).(map[string]any)
	uid = int64(createdUser["id"].(float64))
	grant := admin.call("POST", "/admin/grants", map[string]any{"roleId": roleID, "resourceId": resourceID, "scope": "self", "fields": map[string]string{"employee_id": "plain", "total": "plain", "department_id": "plain"}}, 200).(map[string]any)
	grantID = grant["id"].(string)
	userAPI := integrationAPI{t: t, base: apiURL}.login(username, password)
	userAPI.call("POST", "/query", map[string]any{"resourceId": resourceID}, 403)
	admin.call("POST", "/admin/identities", map[string]any{"userId": uid, "datasourceId": datasourceID, "tenantId": tenant, "externalTenantId": "acme", "employeeId": "E001", "departmentId": "sales", "warehouseIds": []string{}}, 200)
	result := userAPI.call("POST", "/query", map[string]any{"resourceId": resourceID}, 200).(map[string]any)
	rows := result["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("dynamic config returned %d rows", len(rows))
	}
	row := rows[0].(map[string]any)
	if row["employee_id"] != "E001" || fmt.Sprint(row["total"]) != "9000.00" {
		t.Fatalf("dynamic identity mapping failed: %#v", row)
	}
	t.Log("new datasource → introspection → resource → role → new internal tenant user → external acme identity → E001 salary 9000: passed")
	conv := userAPI.call("POST", "/conversations", map[string]any{"runtime": "pi", "title": "integration-role-revocation"}, 200).(map[string]any)
	cid := conv["id"].(string)
	admin.call("PUT", fmt.Sprintf("/admin/users/%d/roles", uid), map[string]any{"roleIds": []int64{}}, 200)
	userAPI.call("GET", "/conversations/"+cid+"/messages", nil, 409)
	userAPI.call("POST", "/query", map[string]any{"resourceId": resourceID}, 403)
	admin.call("PUT", fmt.Sprintf("/admin/users/%d/roles", uid), map[string]any{"roleIds": []int64{roleID}}, 200)
	t.Log("role removal denies query and freezes old conversation: passed")

	started := make(chan string, 1)
	release := make(chan struct{})
	runnerErrors := make(chan string, 1)
	fakeRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/cancel") {
			w.WriteHeader(200)
			return
		}
		if r.URL.Path != "/run" {
			w.WriteHeader(404)
			return
		}
		var body struct {
			MCPURL   string `json:"mcpUrl"`
			MCPToken string `json:"mcpToken"`
		}
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			runnerErrors <- "decode runner payload"
			return
		}
		rpc, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "query_resource", "arguments": map[string]any{"resourceId": resourceID}}})
		request, _ := http.NewRequestWithContext(r.Context(), "POST", body.MCPURL, bytes.NewReader(rpc))
		request.Header.Set("Authorization", "Bearer "+body.MCPToken)
		request.Header.Set("Content-Type", "application/json")
		response, e := http.DefaultClient.Do(request)
		if e != nil {
			runnerErrors <- "MCP request failed"
			return
		}
		raw, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || !strings.Contains(string(raw), "9000.00") {
			runnerErrors <- "real MCP salary not present"
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: status\ndata: {\"message\":\"authorized data read; waiting for test barrier\"}\n\n")
		w.(http.Flusher).Flush()
		started <- string(raw)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		data, _ := json.Marshal(map[string]any{"text": "SHOULD-NOT-PUBLISH " + string(raw)})
		fmt.Fprintf(w, "event: completed\ndata: %s\n\n", data)
	}))
	defer fakeRunner.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	key, e := loadKey()
	if e != nil {
		t.Fatal(e)
	}
	tokens := security.NewTokenService("integration-isolated-token-secret", time.Hour)
	s := &Server{DB: pg, Tokens: tokens, key: key, DataDir: t.TempDir(), RunnerURL: fakeRunner.URL, RunnerSecret: "integration-only", client: &http.Client{Timeout: 10 * time.Second}, active: map[string]context.CancelFunc{}, subscribers: map[string][]chan Event{}, loginAttempts: map[string]*loginAttempt{}}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())
	s.Register(router)
	dedicated := httptest.NewServer(router)
	defer dedicated.Close()
	s.PublicURL = dedicated.URL
	localAPI := integrationAPI{t: t, base: dedicated.URL}.login(username, password)
	newConv := localAPI.call("POST", "/conversations", map[string]any{"runtime": "pi", "title": "integration-inflight-revocation"}, 200).(map[string]any)
	newCID := newConv["id"].(string)
	run := localAPI.call("POST", "/conversations/"+newCID+"/messages", map[string]any{"content": "读取我的工资，等待测试屏障再发布。"}, 200).(map[string]any)
	rid := run["runId"].(string)
	select {
	case <-started:
	case message := <-runnerErrors:
		t.Fatal(message)
	case <-time.After(8 * time.Second):
		t.Fatal("fake runner did not read real MCP data")
	}
	admin.call("DELETE", "/admin/grants/"+grantID, nil, 200)
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	var status, answer string
	for time.Now().Before(deadline) {
		if e = pg.QueryRow(`SELECT status,answer FROM novex_runs WHERE id=$1`, rid).Scan(&status, &answer); e != nil {
			t.Fatal(e)
		}
		if status == "cancelled" || status == "error" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status == "completed" || answer != "" {
		t.Fatalf("revoked answer was persisted: status=%s", status)
	}
	if status != "cancelled" && status != "error" {
		t.Fatalf("run did not terminate after revocation: %s", status)
	}
	var assistantCount int
	if e = pg.QueryRow(`SELECT count(*) FROM novex_messages WHERE run_id=$1 AND role='assistant'`, rid).Scan(&assistantCount); e != nil {
		t.Fatal(e)
	}
	if assistantCount != 0 {
		t.Fatal("revoked answer was saved to conversation")
	}
	localAPI.call("GET", "/conversations/"+newCID+"/messages", nil, 409)
	t.Log("in-flight real MCP salary read → grant revoked → attempted completed output discarded, no assistant message persisted: passed")
}
