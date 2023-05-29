package fastsocket

import (
	"github.com/gorilla/websocket"
	"github.com/zehongyang/utils/config"
	"github.com/zehongyang/utils/logger"
	"go.uber.org/zap"
	"net/http"
	"sync"
	"time"
)

type WebSocketSession struct {
	uid     int64
	login   bool
	data    chan []byte
	conn    *websocket.Conn
	closed  bool
	engine  *WebSocketEngine
	msgNums int
}

func (s *WebSocketSession) Close() error {
	err := s.engine.sm.close(s.uid)
	if err != nil {
		logger.E("WebSocketSession close", zap.Error(err))
	}
	return s.close()
}

func (s *WebSocketSession) close() error {
	return s.conn.Close()
}

func (s *WebSocketSession) read() {
	conn := s.conn
	defer func() {
		err := s.Close()
		if err != nil {
			logger.E("WebSocketSession read", zap.Error(err))
		}
	}()
	for {
		err := conn.SetReadDeadline(time.Now().Add(time.Duration(s.engine.config.ReadTimeout) * time.Second))
		if err != nil {
			logger.E("WebSocketSession read", zap.Error(err))
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			logger.E("WebSocketSession read", zap.Error(err))
			return
		}
		s.msgNums++
		if s.msgNums > 10 && !s.login {
			return
		}
		var ctx = WebSocketContext{
			session: s,
			data:    data,
		}
		err = s.engine.handler.Handle(&ctx)
		if err != nil {
			logger.E("WebSocketSession read", zap.Error(err))
		}
	}
}

func (s *WebSocketSession) write() {
	conn := s.conn
	defer func() {
		err := s.Close()
		if err != nil {
			logger.E("WebSocketSession write", zap.Error(err), zap.Any("session", s))
		}
	}()
	for {
		select {
		case data, ok := <-s.data:
			if !ok {
				return
			}
			err := conn.SetWriteDeadline(time.Now().Add(time.Second * time.Duration(s.engine.config.WriteTimeout)))
			if err != nil {
				logger.E("WebSocketSession write", zap.Error(err), zap.Any("session", s))
			}
			err = conn.WriteMessage(websocket.TextMessage, data)
			if err != nil {
				logger.E("WebSocketSession write", zap.Error(err), zap.Any("session", s))
				return
			}
		}
	}
}

type OptionFunc func(*WebSocketConfig)

type WebSocketConfig struct {
	Port         string
	ReadTimeout  int64
	WriteTimeout int64
	Bucket       int
}

func SetPort(port string) OptionFunc {
	return func(config *WebSocketConfig) {
		config.Port = port
	}
}

func SetReadTimeOut(timeout int64) OptionFunc {
	return func(config *WebSocketConfig) {
		config.ReadTimeout = timeout
	}
}

func SetWriteTimeout(timeout int64) OptionFunc {
	return func(config *WebSocketConfig) {
		config.WriteTimeout = timeout
	}
}

func SetBucket(bucket int) OptionFunc {
	return func(config *WebSocketConfig) {
		config.Bucket = bucket
	}
}

type AppWebSocketConfig struct {
	WebSocket WebSocketConfig
}

var GetWebSocketConfig = func() func() *WebSocketConfig {
	var (
		once sync.Once
		cfg  = WebSocketConfig{Bucket: 16, Port: ":8000", ReadTimeout: 50, WriteTimeout: 10}
	)
	return func() *WebSocketConfig {
		once.Do(func() {
			err := config.Load(&cfg)
			if err != nil {
				logger.F("GetWebSocketConfig", zap.Error(err))
			}
		})
		return &cfg
	}
}()

type WebSocketManager struct {
	sessionManager []*WebSocketSessionManager
}

func (s *WebSocketManager) get(uid int64) (*WebSocketSession, error) {
	idx, err := s.getIndex(uid)
	if err != nil {
		return nil, err
	}
	session := s.sessionManager[idx].getSession(uid)
	return session, nil
}

func (s *WebSocketManager) insert(session *WebSocketSession) error {
	if session == nil || session.uid < 1 {
		return ErrSessionNil
	}
	idx, err := s.getIndex(session.uid)
	if err != nil {
		return err
	}
	return s.sessionManager[idx].insert(session)
}

func (s *WebSocketManager) write(uid int64, data []byte) error {
	idx, err := s.getIndex(uid)
	if err != nil {
		return err
	}
	return s.sessionManager[idx].write(uid, data)
}

func (s *WebSocketManager) getIndex(uid int64) (int64, error) {
	if uid < 1 {
		return 0, ErrSessionManagerNotFound
	}
	length := int64(len(s.sessionManager))
	idx := uid % length
	if idx < 0 || idx >= length {
		return 0, ErrSessionManagerNotFound
	}
	return idx, nil
}

func (s *WebSocketManager) close(uid int64) error {
	idx, err := s.getIndex(uid)
	if err != nil {
		return err
	}
	return s.sessionManager[idx].close(uid)
}

func (s *WebSocketEngine) newWebSocketManager() *WebSocketEngine {
	sms := make([]*WebSocketSessionManager, s.config.Bucket)
	for i := 0; i < s.config.Bucket; i++ {
		sms[i] = &WebSocketSessionManager{sessions: make(map[int64]*WebSocketSession)}
	}
	s.sm = &WebSocketManager{sessionManager: sms}
	return s
}

type WebSocketSessionManager struct {
	mu       sync.RWMutex
	sessions map[int64]*WebSocketSession
}

func (s *WebSocketSessionManager) getSession(uid int64) *WebSocketSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[uid]
}

func (s *WebSocketSessionManager) insert(session *WebSocketSession) error {
	if session == nil || session.uid < 1 {
		return ErrSessionNil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.sessions[session.uid]
	if ok {
		err := old.close()
		if err != nil {
			logger.E("WebSocketSessionManager insert", zap.Error(err), zap.Any("session", session))
		}
	}
	s.sessions[session.uid] = session
	return nil
}

func (s *WebSocketSessionManager) write(uid int64, data []byte) error {
	if uid < 1 || len(data) < 1 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[uid]
	if !ok {
		return nil
	}
	session.data <- data
	return nil
}

func (s *WebSocketSessionManager) close(uid int64) error {
	if uid < 1 {
		return ErrSessionManagerNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[uid]
	if !ok {
		return ErrSessionManagerNotFound
	}
	if session.closed {
		return nil
	}
	session.closed = true
	close(session.data)
	delete(s.sessions, uid)
	return nil
}

type WebSocketEngine struct {
	config  *WebSocketConfig
	sm      *WebSocketManager
	handler IHandler
}

func New(options ...OptionFunc) *WebSocketEngine {
	cfg := GetWebSocketConfig()
	if len(options) > 0 {
		for _, optionFunc := range options {
			optionFunc(cfg)
		}
	}
	enging := WebSocketEngine{config: cfg}
	enging.newWebSocketManager()
	return &enging
}

func (e *WebSocketEngine) Serve(handler IHandler) (err error) {
	if handler == nil {
		return ErrHandlerNil
	}
	e.handler = handler
	upgrader := websocket.Upgrader{WriteBufferSize: 1024, ReadBufferSize: 1024}
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		var (
			conn *websocket.Conn
		)
		conn, err = upgrader.Upgrade(writer, request, nil)
		if err != nil {
			logger.E("WebSocketEngine Serve", zap.Error(err))
			return
		}
		ctx := &WebSocketSession{conn: conn, engine: e, data: make(chan []byte, 1024)}
		go ctx.read()
		go ctx.write()
	})
	logger.I("WebSocket Listening On Port", zap.Any("port", e.config.Port))
	return http.ListenAndServe(e.config.Port, nil)
}

type IHandler interface {
	Handle(ctx *WebSocketContext) error
}
