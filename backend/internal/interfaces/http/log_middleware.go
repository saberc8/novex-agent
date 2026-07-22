package http

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"voc-go-backend/internal/domain/syslog"
	"voc-go-backend/internal/infrastructure/security"
)

// sysLogMiddleware 负责在 HTTP 层统一采集请求/响应信息并写入 sys_log。
// 设计目标：
//   - 尽量贴近 Java 版 LogDaoLocalImpl 的字段含义；
//   - 只记录业务接口（跳过 OPTIONS 等预检请求），减少无效数据；
//   - 出错时不影响业务请求主流程。
type sysLogMiddleware struct {
	repo     syslog.Repository
	tokenSvc *security.TokenService
}

// NewSysLogMiddleware 创建 Gin 中间件，用于记录系统操作日志。
func NewSysLogMiddleware(repo syslog.Repository, tokenSvc *security.TokenService) gin.HandlerFunc {
	m := &sysLogMiddleware{
		repo:     repo,
		tokenSvc: tokenSvc,
	}
	return m.handle
}

// bodyCaptureWriter 包装 ResponseWriter，用于捕获响应状态码和响应体。
type bodyCaptureWriter struct {
	gin.ResponseWriter
	status int
	body   cappedBuffer
}

func (w *bodyCaptureWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *bodyCaptureWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	_, _ = w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

type cappedBuffer struct {
	bytes.Buffer
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	originalLength := len(p)
	remaining := maxLogBodyLength - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return originalLength, nil
}

type teeReadCloser struct {
	io.Reader
	io.Closer
}

// handle 是实际的中间件逻辑。
func (m *sysLogMiddleware) handle(c *gin.Context) {
	// 仅记录业务请求，跳过浏览器预检。
	if c.Request.Method == http.MethodOptions {
		c.Next()
		return
	}

	start := time.Now()

	// 读取并保留请求体，避免影响后续 handler。
	var requestCapture cappedBuffer
	if c.Request.Body != nil && !strings.Contains(strings.ToLower(c.ContentType()), "multipart/form-data") {
		originalBody := c.Request.Body
		c.Request.Body = &teeReadCloser{
			Reader: io.TeeReader(originalBody, &requestCapture),
			Closer: originalBody,
		}
	}

	// 包装响应 writer，捕获状态码与响应内容。
	origWriter := c.Writer
	cw := &bodyCaptureWriter{ResponseWriter: origWriter}
	c.Writer = cw

	// 执行后续处理。
	c.Next()
	// 部分处理器不会主动读取请求体；此时在请求结束后继续消费剩余内容，
	// 由 cappedBuffer 保证审计日志最多保留固定长度。
	if c.Request.Body != nil {
		_, _ = io.Copy(io.Discard, c.Request.Body)
	}

	duration := time.Since(start)

	// 如果没有仓储实现，直接返回，不影响主流程。
	if m.repo == nil {
		return
	}

	// 组装日志记录。
	path := c.FullPath()
	if path == "" {
		path = c.Request.URL.Path
	}

	rec := &syslog.Record{
		RequestURL:     c.Request.URL.String(),
		RequestMethod:  c.Request.Method,
		RequestHeaders: marshalHeaders(c.Request.Header),
		RequestBody:    sanitizeLogPayload(requestCapture.Bytes(), c.ContentType()),
		StatusCode:     statusFromWriter(cw),
		ResponseHeaders: marshalHeaders(
			http.Header(cw.Header()),
		),
		ResponseBody: sanitizeLogPayload(cw.body.Bytes(), cw.Header().Get("Content-Type")),
		TimeTaken:    duration.Milliseconds(),
		IP:           truncateString(c.ClientIP(), 100),
		Address:      "",
		Browser:      truncateString(c.Request.UserAgent(), 100),
		OS:           "",
		CreateTime:   start,
	}

	// 根据响应状态设置业务状态，后续如需更复杂逻辑可扩展（例如解析 APIResponse）。
	if rec.StatusCode >= http.StatusBadRequest {
		rec.Status = syslog.StatusFailure
	} else {
		rec.Status = syslog.StatusSuccess
	}

	// 从 Authorization 头解析登录用户 ID。
	if m.tokenSvc != nil {
		if authz := c.GetHeader("Authorization"); authz != "" {
			if claims, err := m.tokenSvc.ParseContext(c.Request.Context(), authz); err == nil && claims.UserID != 0 {
				uid := claims.UserID
				rec.CreateUser = &uid
			}
		}
	}

	// 按 URL 粗略推断模块与描述，先提供基础能力，后续可按需细化。
	rec.Module, rec.Description = inferModuleAndDescription(path, c.Request.Method)

	// 最终落库，错误不影响业务，但打印错误便于排查。
	if err := m.repo.Save(c.Request.Context(), rec); err != nil {
		// 仅打印日志，不向前端暴露内部错误。
		log.Printf("[syslog] save failed: method=%s path=%s status=%d err=%v",
			c.Request.Method, path, rec.StatusCode, err)
	}
}

// marshalHeaders 将 HTTP Header 转为 JSON 字符串，便于前端调试查看。
func marshalHeaders(h http.Header) string {
	if len(h) == 0 {
		return ""
	}
	m := make(map[string]string, len(h))
	for k, v := range h {
		if isSensitiveLogKey(k) {
			m[k] = redactedLogValue
			continue
		}
		m[k] = strings.Join(v, ",")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return truncateString(string(b), maxLogBodyLength)
}

const (
	redactedLogValue = "***"
	maxLogBodyLength = 8192
)

func sanitizeLogPayload(raw []byte, contentType string) string {
	contentType = strings.ToLower(contentType)
	if strings.Contains(contentType, "multipart/form-data") {
		return "[multipart body omitted]"
	}
	if len(raw) == 0 {
		return ""
	}
	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		values, err := url.ParseQuery(string(raw))
		if err == nil {
			for key := range values {
				if isSensitiveLogKey(key) {
					values.Set(key, redactedLogValue)
				}
			}
			return truncateString(values.Encode(), maxLogBodyLength)
		}
		return "[form body omitted: invalid or truncated]"
	}

	var body any
	if err := json.Unmarshal(raw, &body); err == nil {
		redactLogValue(body)
		sanitized, err := json.Marshal(body)
		if err == nil {
			return truncateString(string(sanitized), maxLogBodyLength)
		}
	}
	if strings.Contains(contentType, "json") {
		return "[json body omitted: invalid or truncated]"
	}
	return truncateString(string(raw), maxLogBodyLength)
}

func redactLogValue(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if isSensitiveLogKey(key) {
				v[key] = redactedLogValue
				continue
			}
			redactLogValue(item)
		}
	case []any:
		for _, item := range v {
			redactLogValue(item)
		}
	}
}

func isSensitiveLogKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(key))
	switch normalized {
	case "authorization", "cookie", "setcookie", "password", "passwd", "pwd", "token", "accesstoken", "refreshtoken", "secret", "secretkey", "accesskey", "privatekey", "captcha", "captchaverification":
		return true
	}
	return strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "passwd") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "privatekey") ||
		strings.Contains(normalized, "captchaverification")
}

// statusFromWriter 获取最终 HTTP 状态码，未显式设置时默认为 200。
func statusFromWriter(w *bodyCaptureWriter) int {
	if w == nil || w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// inferModuleAndDescription 根据路由前缀推断所属模块和简单描述。
// 这里仅做基础映射，用于满足前端系统日志展示需求。
func inferModuleAndDescription(path, method string) (module string, desc string) {
	switch {
	case strings.HasPrefix(path, "/auth/login"):
		return "登录", "用户登录"
	case strings.HasPrefix(path, "/auth/logout"):
		return "登录", "用户退出登录"
	case strings.HasPrefix(path, "/system/user"):
		return "用户管理", method + " /system/user"
	case strings.HasPrefix(path, "/system/role"):
		return "角色管理", method + " /system/role"
	case strings.HasPrefix(path, "/system/dept"):
		return "部门管理", method + " /system/dept"
	case strings.HasPrefix(path, "/system/menu"):
		return "菜单管理", method + " /system/menu"
	case strings.HasPrefix(path, "/system/dict"):
		return "字典管理", method + " /system/dict"
	case strings.HasPrefix(path, "/system/config"):
		return "系统配置", method + " /system/config"
	case strings.HasPrefix(path, "/system/storage"):
		return "存储配置", method + " /system/storage"
	case strings.HasPrefix(path, "/system/client"):
		return "客户端配置", method + " /system/client"
	case strings.HasPrefix(path, "/monitor/online"):
		return "在线用户", method + " /monitor/online"
	case strings.HasPrefix(path, "/monitor/log"):
		return "系统日志", method + " /monitor/log"
	default:
		return "其它", method + " " + path
	}
}

// truncateString 用于避免写入超过数据库字段长度的字符串。
func truncateString(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max]
}
