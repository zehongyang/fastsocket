package fastsocket

import "testing"

type Hd struct {
}

func (s *Hd) Handle(ctx *WebSocketContext) error {
	return nil
}

func TestSocket(t *testing.T) {
	engine := New()
	var hd Hd
	engine.Handle(&hd)
	err := engine.Serve()
	t.Log(err)
}
