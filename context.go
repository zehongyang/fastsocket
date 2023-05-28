package fastsocket

type WebSocketContext struct {
	session *WebSocketSession
	data    []byte
}

func (s *WebSocketContext) Read() []byte {
	return s.data
}

func (s *WebSocketContext) Write(uid int64, data []byte) error {
	if uid == s.session.uid {
		s.session.data <- data
		return nil
	}
	return s.session.engine.sm.write(uid, data)
}

func (s *WebSocketContext) Close() error {
	return s.session.Close()
}
