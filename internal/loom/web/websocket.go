package web

import (
	"net/http"

	"github.com/coder/websocket"
)

// AcceptWebSocket preserves the library's default same-origin check and sets
// the route's read limit. Tickets, terminal processes and frame handling belong
// to the application.
func AcceptWebSocket(w http.ResponseWriter, r *http.Request, readLimit int64) (*websocket.Conn, error) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(readLimit)
	return conn, nil
}
