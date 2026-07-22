package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"voc-go-backend/internal/infrastructure/security"
)

// OnlineSession 表示一条在线会话记录（基于内存的简单实现）。
type OnlineSession struct {
	UserID         int64     `json:"userId"`
	Username       string    `json:"username"`
	Nickname       string    `json:"nickname"`
	Token          string    `json:"token"`
	ClientType     string    `json:"clientType"`
	ClientID       string    `json:"clientId"`
	IP             string    `json:"ip"`
	Address        string    `json:"address"`
	Browser        string    `json:"browser"`
	OS             string    `json:"os"`
	LoginTime      time.Time `json:"loginTime"`
	LastActiveTime time.Time `json:"lastActiveTime"`
}

// OnlineUserResp 与前端 OnlineUserResp 类型对齐。
type OnlineUserResp struct {
	ID             int64  `json:"id"`
	Token          string `json:"token"`
	Username       string `json:"username"`
	Nickname       string `json:"nickname"`
	ClientType     string `json:"clientType"`
	ClientID       string `json:"clientId"`
	IP             string `json:"ip"`
	Address        string `json:"address"`
	Browser        string `json:"browser"`
	OS             string `json:"os"`
	LoginTime      string `json:"loginTime"`
	LastActiveTime string `json:"lastActiveTime"`
}

// OnlineStore maintains online-session display data in Redis, with an
// in-memory fallback used by isolated tests.
type OnlineStore struct {
	mu       sync.RWMutex
	sessions map[string]*OnlineSession
	redis    *redis.Client
	tokens   *security.TokenService
}

const onlineSessionIndexKey = "ONLINE_SESSION_INDEX"

// NewOnlineStore 创建内存在线会话存储。
func NewOnlineStore() *OnlineStore {
	return &OnlineStore{
		sessions: make(map[string]*OnlineSession),
	}
}

// NewRedisOnlineStore creates a cross-instance online-session view. The JWT
// session itself remains authoritative; stale display records are discarded.
func NewRedisOnlineStore(client *redis.Client, tokens *security.TokenService) *OnlineStore {
	return &OnlineStore{
		sessions: make(map[string]*OnlineSession),
		redis:    client,
		tokens:   tokens,
	}
}

// RecordLogin 在用户登录成功后记录在线会话信息。
func (s *OnlineStore) RecordLogin(c *gin.Context, userID int64, username, nickname, clientID, token string) {
	if userID == 0 || token == "" {
		return
	}
	now := time.Now()
	ip := c.ClientIP()
	ua := c.Request.UserAgent()

	session := &OnlineSession{
		UserID:         userID,
		Username:       username,
		Nickname:       nickname,
		Token:          token,
		ClientType:     "PC",
		ClientID:       clientID,
		IP:             ip,
		Address:        "",
		Browser:        ua,
		OS:             "",
		LoginTime:      now,
		LastActiveTime: now,
	}
	if s.redis != nil {
		if err := s.saveRedisSession(c.Request.Context(), session); err != nil {
			log.Printf("[online] record login failed: %v", err)
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = session
}

// RemoveByToken 根据 token 移除在线会话。
func (s *OnlineStore) RemoveByToken(ctx context.Context, token string) {
	if token == "" {
		return
	}
	if s.redis != nil {
		key := onlineSessionKey(token)
		if _, err := s.redis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Del(ctx, key)
			pipe.SRem(ctx, onlineSessionIndexKey, key)
			return nil
		}); err != nil {
			log.Printf("[online] remove session failed: %v", err)
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func (s *OnlineStore) Touch(ctx context.Context, token string) {
	if token == "" {
		return
	}
	if s.redis != nil {
		key := onlineSessionKey(token)
		encoded, err := s.redis.Get(ctx, key).Bytes()
		if err != nil {
			return
		}
		var session OnlineSession
		if json.Unmarshal(encoded, &session) != nil {
			return
		}
		session.LastActiveTime = time.Now()
		if err := s.saveRedisSession(ctx, &session); err != nil {
			log.Printf("[online] touch session failed: %v", err)
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[token]; ok {
		session.LastActiveTime = time.Now()
	}
}

func NewOnlineActivityMiddleware(store *OnlineStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.HasPrefix(strings.ToLower(token), "bearer ") {
			token = strings.TrimSpace(token[7:])
		}
		if store != nil && token != "" {
			store.Touch(c.Request.Context(), token)
		}
		c.Next()
	}
}

// List 返回按登录时间倒序的在线用户分页结果。
func (s *OnlineStore) List(ctx context.Context, nickname string, loginStart, loginEnd *time.Time, page, size int) ([]OnlineUserResp, int64, error) {
	sessions, err := s.allSessions(ctx)
	if err != nil {
		return nil, 0, err
	}

	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}

	var filtered []*OnlineSession
	for _, sess := range sessions {
		if nickname != "" &&
			!strings.Contains(sess.Username, nickname) &&
			!strings.Contains(sess.Nickname, nickname) {
			continue
		}
		if loginStart != nil && sess.LoginTime.Before(*loginStart) {
			continue
		}
		if loginEnd != nil && sess.LoginTime.After(*loginEnd) {
			continue
		}
		filtered = append(filtered, sess)
	}

	// 按登录时间倒序排序。
	sort.Slice(filtered, func(left, right int) bool {
		return filtered[left].LoginTime.After(filtered[right].LoginTime)
	})

	total := int64(len(filtered))
	start := (page - 1) * size
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + size
	if end > len(filtered) {
		end = len(filtered)
	}

	result := make([]OnlineUserResp, 0, end-start)
	for _, sess := range filtered[start:end] {
		result = append(result, OnlineUserResp{
			ID:             sess.UserID,
			Token:          sess.Token,
			Username:       sess.Username,
			Nickname:       sess.Nickname,
			ClientType:     sess.ClientType,
			ClientID:       sess.ClientID,
			IP:             sess.IP,
			Address:        sess.Address,
			Browser:        sess.Browser,
			OS:             sess.OS,
			LoginTime:      formatTime(sess.LoginTime),
			LastActiveTime: formatTime(sess.LastActiveTime),
		})
	}

	return result, total, nil
}

func (s *OnlineStore) allSessions(ctx context.Context) ([]*OnlineSession, error) {
	if s.redis == nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		result := make([]*OnlineSession, 0, len(s.sessions))
		for _, session := range s.sessions {
			copyValue := *session
			result = append(result, &copyValue)
		}
		return result, nil
	}
	keys, err := s.redis.SMembers(ctx, onlineSessionIndexKey).Result()
	if err != nil || len(keys) == 0 {
		return nil, err
	}
	values, err := s.redis.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	result := make([]*OnlineSession, 0, len(values))
	for index, raw := range values {
		encoded, ok := raw.(string)
		if !ok {
			_ = s.redis.SRem(ctx, onlineSessionIndexKey, keys[index]).Err()
			continue
		}
		var session OnlineSession
		if err := json.Unmarshal([]byte(encoded), &session); err != nil {
			_ = s.redis.Del(ctx, keys[index]).Err()
			_ = s.redis.SRem(ctx, onlineSessionIndexKey, keys[index]).Err()
			continue
		}
		if s.tokens != nil {
			active, err := s.tokens.IsSessionActive(ctx, session.Token)
			if err != nil {
				return nil, err
			}
			if !active {
				s.RemoveByToken(ctx, session.Token)
				continue
			}
		}
		result = append(result, &session)
	}
	return result, nil
}

func (s *OnlineStore) saveRedisSession(ctx context.Context, session *OnlineSession) error {
	encoded, err := json.Marshal(session)
	if err != nil {
		return err
	}
	key := onlineSessionKey(session.Token)
	_, err = s.redis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, key, encoded, 0)
		pipe.SAdd(ctx, onlineSessionIndexKey, key)
		return nil
	})
	return err
}

func onlineSessionKey(token string) string {
	digest := sha256.Sum256([]byte(token))
	return "ONLINE_SESSION:" + hex.EncodeToString(digest[:])
}

// OnlineUserHandler 提供 /monitor/online 相关接口。
type OnlineUserHandler struct {
	store    *OnlineStore
	tokenSvc *security.TokenService
}

// NewOnlineUserHandler 创建在线用户 handler。
func NewOnlineUserHandler(store *OnlineStore, tokenSvc *security.TokenService) *OnlineUserHandler {
	return &OnlineUserHandler{
		store:    store,
		tokenSvc: tokenSvc,
	}
}

// RegisterOnlineUserRoutes 注册在线用户路由。
func (h *OnlineUserHandler) RegisterOnlineUserRoutes(r *gin.Engine) {
	r.GET("/monitor/online", h.PageOnlineUser)
	r.DELETE("/monitor/online/:token", h.Kickout)
}

// PageOnlineUser 处理 GET /monitor/online，返回分页在线用户列表。
func (h *OnlineUserHandler) PageOnlineUser(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("size"))
	nickname := strings.TrimSpace(c.Query("nickname"))

	var (
		startTime *time.Time
		endTime   *time.Time
	)

	timeRange := c.QueryArray("loginTime")
	if len(timeRange) == 2 {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", timeRange[0], time.Local); err == nil {
			startTime = &t
		}
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", timeRange[1], time.Local); err == nil {
			endTime = &t
		}
	}

	list, total, err := h.store.List(c.Request.Context(), nickname, startTime, endTime, page, size)
	if err != nil {
		Fail(c, "500", "查询在线用户失败")
		return
	}
	OK(c, PageResult[OnlineUserResp]{List: list, Total: total})
}

// Kickout 处理 DELETE /monitor/online/:token，将指定 token 标记为下线。
func (h *OnlineUserHandler) Kickout(c *gin.Context) {
	token := c.Param("token")
	if strings.TrimSpace(token) == "" {
		Fail(c, "400", "令牌不能为空")
		return
	}

	authz := c.GetHeader("Authorization")
	raw := strings.TrimSpace(authz)
	if strings.HasPrefix(strings.ToLower(raw), "bearer ") {
		raw = strings.TrimSpace(raw[7:])
	}
	currentToken := raw

	if currentToken != "" && currentToken == token {
		Fail(c, "400", "不能强退自己")
		return
	}

	// 鉴权：仅需要校验当前请求 token 是否有效。
	if authz == "" {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	if _, err := h.tokenSvc.ParseContext(c.Request.Context(), authz); err != nil {
		Fail(c, "401", "未授权，请重新登录")
		return
	}

	if err := h.tokenSvc.RevokeSession(c.Request.Context(), token); err != nil {
		Fail(c, "500", "强退用户失败")
		return
	}
	h.store.RemoveByToken(c.Request.Context(), token)
	OK(c, true)
}
