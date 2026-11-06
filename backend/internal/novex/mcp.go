package novex

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (s *Server) mcp(c *gin.Context) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	token := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	var uid, version int64
	var tenant string
	e := s.DB.QueryRowContext(c, `SELECT user_id,tenant_id,policy_version FROM novex_runs WHERE token_hash=$1 AND status='running' AND created_at>now()-interval '15 minutes'`, tokenHash(token)).Scan(&uid, &tenant, &version)
	if e != nil {
		fail(c, 401, "运行凭据无效或已失效")
		return
	}
	u, e := s.loadUser(c, uid)
	if e != nil || u.TenantID != tenant || u.PolicyVersion != version {
		fail(c, 403, "运行权限版本已失效")
		return
	}
	var req rpcRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	if e = c.ShouldBindJSON(&req); e != nil {
		c.JSON(400, gin.H{"jsonrpc": "2.0", "id": nil, "error": gin.H{"code": -32700, "message": "Parse error"}})
		return
	}
	reply := func(v any) { c.JSON(200, gin.H{"jsonrpc": "2.0", "id": req.ID, "result": v}) }
	rpcError := func(code int, msg string) {
		c.JSON(200, gin.H{"jsonrpc": "2.0", "id": req.ID, "error": gin.H{"code": code, "message": msg}})
	}
	if len(req.ID) == 0 {
		c.Status(202)
		return
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := p.ProtocolVersion
		if !contains([]string{"2024-11-05", "2025-03-26", "2025-06-18"}, version) {
			version = "2025-03-26"
		}
		reply(gin.H{"protocolVersion": version, "capabilities": gin.H{"tools": gin.H{}}, "serverInfo": gin.H{"name": "novex-business", "version": "0.1.0"}, "instructions": "Use only these authorized tools and mounted knowledge. Caller identity is immutable. Never claim a database fact without a successful tool response."})
	case "ping":
		reply(gin.H{})
	case "tools/list":
		reply(gin.H{"tools": mcpTools()})
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &p) != nil {
			rpcError(-32602, "Invalid params")
			return
		}
		var value any
		var err error
		arguments := p.Arguments
		switch p.Name {
		case "list_resources":
			value, err = s.visible(c, u)
		case "query_resource":
			var q Query
			if json.Unmarshal(arguments, &q) != nil {
				err = ErrDenied
			} else {
				q.UserID = u.ID
				value, err = s.queryUnlocked(c, u, q)
			}
		case "search_documents":
			var p struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if json.Unmarshal(arguments, &p) != nil || blank(p.Query) {
				err = ErrDenied
				break
			}
			if p.Limit <= 0 || p.Limit > 20 {
				p.Limit = 10
			}
			value, err = s.searchDocuments(c, u, p.Query, p.Limit)
		case "query_spreadsheet":
			var p struct {
				DocumentID string `json:"documentId"`
				SheetQuery
			}
			if json.Unmarshal(arguments, &p) != nil {
				err = ErrDenied
			} else {
				value, err = s.querySpreadsheet(c, u, p.DocumentID, p.SheetQuery)
			}
		case "read_document":
			var p struct {
				DocumentID string `json:"documentId"`
			}
			if json.Unmarshal(arguments, &p) != nil {
				err = ErrDenied
			} else {
				value, err = s.document(c, u, p.DocumentID)
			}
		default:
			rpcError(-32602, "Unknown tool")
			return
		}
		if err != nil {
			reply(gin.H{"content": []gin.H{{"type": "text", "text": err.Error()}}, "isError": true})
			return
		}
		encoded, _ := json.Marshal(value)
		reply(gin.H{"content": []gin.H{{"type": "text", "text": string(encoded)}}, "isError": false})
	default:
		rpcError(-32601, "Method not found")
	}
}
func mcpTools() []gin.H {
	str := func(description string) gin.H { return gin.H{"type": "string", "description": description} }
	obj := func(properties gin.H, required []string) gin.H {
		return gin.H{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	return []gin.H{
		{"name": "list_resources", "description": "List business data resources and fields available to the current authenticated caller. Never invent a resourceId.", "inputSchema": obj(gin.H{}, []string{})},
		{"name": "query_resource", "description": "Read authorized rows and fields of a registered business resource. Tenant and actor scope are enforced server-side. Hidden fields cannot be filtered or sorted. Use list_resources first.", "inputSchema": obj(gin.H{"resourceId": str("Resource id or key from list_resources"), "fields": gin.H{"type": "array", "items": str("Column name")}, "filters": gin.H{"type": "array", "items": obj(gin.H{"field": str("Column"), "op": gin.H{"type": "string", "enum": []string{"eq", "contains", "gte", "lte"}}, "value": gin.H{}}, []string{"field", "op", "value"})}, "sort": obj(gin.H{"field": str("Column"), "direction": gin.H{"type": "string", "enum": []string{"asc", "desc"}}}, []string{"field", "direction"}), "limit": gin.H{"type": "integer", "minimum": 1, "maximum": 100}}, []string{"resourceId"})},
		{"name": "search_documents", "description": "Indexed literal keyword search in authorized Markdown, workbook titles and Excel/CSV cells. No vector database. Spreadsheet matches identify the workbook; use read_document for its sheet/column schema then query_spreadsheet for rows.", "inputSchema": obj(gin.H{"query": str("Keyword, product SKU or exact phrase; split a long natural-language question into focused keywords"), "limit": gin.H{"type": "integer", "minimum": 1, "maximum": 20}}, []string{"query"})},
		{"name": "read_document", "description": "Read an authorized document. Markdown returns text; spreadsheets return sheet names, typed column keys, row counts and warnings, not the whole workbook. Use query_spreadsheet for data.", "inputSchema": obj(gin.H{"documentId": str("Document id")}, []string{"documentId"})},
		{"name": "query_spreadsheet", "description": "Search original Excel/CSV structured index without converting to Markdown. First read_document to get sheet names and column letter keys. Exact filters use typed JSON indexes; query is a literal keyword across cells. Returns only authorized rows, original Excel row numbers and cell-range citations. Formulas are never executed; missing cached results are warnings. All workbook cells share its document ACL, not database role field grants.", "inputSchema": obj(gin.H{"documentId": str("Authorized workbook id"), "sheet": str("Exact sheet name; required for column filters"), "query": str("Literal keyword/SKU; optional"), "filters": gin.H{"type": "array", "items": obj(gin.H{"column": str("Column key from read_document, e.g. A, B"), "op": gin.H{"type": "string", "enum": []string{"eq", "contains", "gte", "lte"}}, "value": gin.H{}}, []string{"column", "op", "value"})}, "limit": gin.H{"type": "integer", "minimum": 1, "maximum": 100}, "offset": gin.H{"type": "integer", "minimum": 0, "maximum": 200000}}, []string{"documentId"})},
	}
}
