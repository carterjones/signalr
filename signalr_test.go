package signalr_test

import (
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/carterjones/helpers/trace"
	"github.com/carterjones/signalr"
	"github.com/gorilla/websocket"
)

func negotiate(w http.ResponseWriter, r *http.Request) {
	_, err := w.Write([]byte(`{"ConnectionToken":"hello world","ConnectionId":"1234-ABC","URL":"/signalr"}`))
	if err != nil {
		trace.Error(err)
		return
	}
}

func connect(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		trace.Error(err)
		return
	}

	go func() {
		for {
			var msgType int
			var bs []byte
			msgType, bs, err = c.ReadMessage()
			if err != nil {
				trace.Error(err)
				return
			}

			log.Println(msgType, string(bs))
		}
	}()

	go func() {
		for {
			err = c.WriteMessage(websocket.TextMessage, []byte(`{"S":1}`))
			if err != nil {
				trace.Error(err)
				return
			}
		}
	}()
}

func start(w http.ResponseWriter, r *http.Request) {
	_, err := w.Write([]byte(`{"Response":"started"}`))
	if err != nil {
		trace.Error(err)
		return
	}
}

func TestNew(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/negotiate") {
			negotiate(w, r)
		} else if strings.Contains(r.URL.Path, "/connect") {
			connect(w, r)
		} else if strings.Contains(r.URL.Path, "/start") {
			start(w, r)
		} else {
			log.Println("url:", r.URL)
		}
	}))
	defer ts.Close()

	host := strings.TrimPrefix(ts.URL, "http://")

	// Prepare a SignalR client.
	c := signalr.New(host, "1.5", "/signalr", "all the data")
	c.Scheme = signalr.HTTP

	// Initialize the client.
	err := c.Init()
	if err != nil {
		log.Panic(err)
	}
}
