package signalr

import (
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

// TestCompleteHandler combines the negotiate, connect, reconnect, and start
// handlers found in this package into one complete response handler.
func TestCompleteHandler(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.Contains(r.URL.Path, "/negotiate"):
		TestNegotiate(w, r)
	case strings.Contains(r.URL.Path, "/connect"):
		TestConnect(w, r)
	case strings.Contains(r.URL.Path, "/reconnect"):
		TestReconnect(w, r)
	case strings.Contains(r.URL.Path, "/start"):
		TestStart(w, r)
	}
}

// TestNegotiate provides a sample "/negotiate" handling function.
//
// If an error occurs while writing the response data, it will panic.
func TestNegotiate(w http.ResponseWriter, r *http.Request) {
	// nolint:lll
	_, err := w.Write([]byte(`{"ConnectionToken":"hello world","ConnectionId":"1234-ABC","URL":"/signalr","ProtocolVersion":"1337"}`))
	if err != nil {
		panic(err)
	}
}

// TestConnect provides a sample "/connect" handling function.
//
// If an error occurs while upgrading the websocket, it will panic.
func TestConnect(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			_, _, rerr := c.ReadMessage()
			if rerr != nil {
				return
			}
		}
	}()

	go func() {
		for {
			werr := c.WriteMessage(websocket.TextMessage, []byte(`{"S":1}`))
			if werr != nil {
				return
			}
		}
	}()
}

// TestReconnect provides a sample "/reconnect" handling function. It simply
// calls TestConnect.
func TestReconnect(w http.ResponseWriter, r *http.Request) {
	TestConnect(w, r)
}

// TestStart provides a sample "/start" handling function.
//
// If an error occurs while writing the response data, it will panic.
func TestStart(w http.ResponseWriter, r *http.Request) {
	_, err := w.Write([]byte(`{"Response":"started"}`))
	if err != nil {
		panic(err)
	}
}
