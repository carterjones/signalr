package signalr_test

import (
	"crypto/x509"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/carterjones/helpers/trace"
	"github.com/carterjones/signalr"
	"github.com/gorilla/websocket"
)

func equals(tb testing.TB, id string, exp, act interface{}) {
	if !reflect.DeepEqual(exp, act) {
		_, file, line, _ := runtime.Caller(1)
		tb.Errorf("\n\033[31m%s:%d (%s):\n\n\texp: %#v\n\tgot: %#v\033[39m\n\n",
			filepath.Base(file), line, id, exp, act)
	}
}

func notNil(tb testing.TB, id string, act interface{}) {
	if act == nil {
		_, file, line, _ := runtime.Caller(1)
		tb.Errorf("\n\033[31m%s:%d (%s):\n\n\texp: a non-nil value\n\tgot: %#v\033[39m\n\n",
			filepath.Base(file), line, id, act)
	}
}

func hostFromServerURL(url string) (host string) {
	host = strings.TrimPrefix(url, "https://")
	host = strings.TrimPrefix(host, "http://")
	return
}

func newTestServer(fn http.HandlerFunc, tls bool) (ts *httptest.Server) {
	if tls {
		// Create the server.
		ts = httptest.NewTLSServer(fn)

		// Save the testing certificate to the TLS client config.
		//
		// I'm not sure why ts.TLS doesn't contain certificate
		// information. However, this seems to make the testing TLS
		// certificate be trusted by the client.
		ts.TLS.RootCAs = x509.NewCertPool()
		ts.TLS.RootCAs.AddCert(ts.Certificate())
	} else {
		// Create the server.
		ts = httptest.NewServer(fn)
	}

	return
}

func newTestClient(protocol, endpoint, connectionData string, ts *httptest.Server) (c *signalr.Client) {
	// Prepare a SignalR client.
	c = signalr.New(hostFromServerURL(ts.URL), protocol, endpoint, connectionData)
	c.HTTPClient = ts.Client()

	// Save the TLS config in case this is using TLS.
	c.TLSClientConfig = ts.TLS

	return
}

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

func TestClient_Negotiate(t *testing.T) {
}

func TestClient_Connect(t *testing.T) {
}

func TestClient_Start(t *testing.T) {
}

func TestClient_Reconnect(t *testing.T) {
}

func TestClient_Init(t *testing.T) {
	ts := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/negotiate") {
			negotiate(w, r)
		} else if strings.Contains(r.URL.Path, "/connect") {
			connect(w, r)
		} else if strings.Contains(r.URL.Path, "/start") {
			start(w, r)
		} else {
			log.Println("url:", r.URL)
		}
	}), true)
	defer ts.Close()

	c := newTestClient("1.5", "/signalr", "all the data", ts)

	// Initialize the client.
	err := c.Init()
	if err != nil {
		log.Panic(err)
	}

	// TODO: literally any form of validatation
	// TODO: check for specific errors
}

func TestClient_Send(t *testing.T) {
}

func TestClient_Messages(t *testing.T) {
}

func TestNew(t *testing.T) {
	// Define parameter values.
	host := "test-host"
	protocol := "test-protocol"
	endpoint := "test-endpoint"
	connectionData := "test-connection-data"

	// Create the client.
	c := signalr.New(host, protocol, endpoint, connectionData)

	// Validate values were set up properly.
	equals(t, "host", host, c.Host)
	equals(t, "protocol", protocol, c.Protocol)
	equals(t, "endpoint", endpoint, c.Endpoint)
	equals(t, "connection data", connectionData, c.ConnectionData)
	equals(t, "http client", new(http.Client), c.HTTPClient)
	equals(t, "scheme", signalr.HTTPS, c.Scheme)
	equals(t, "max negotiate retries", 5, c.MaxNegotiateRetries)
	notNil(t, "messages", c.Messages())
}
