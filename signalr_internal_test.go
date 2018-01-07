package signalr

import (
	"crypto/x509"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

func red(s string) string {
	return "\033[31m" + s + "\033[39m"
}

// Note: this is largely derived from
// https://github.com/golang/go/blob/1c69384da4fb4a1323e011941c101189247fea67/src/net/http/response_test.go#L915-L940
func testErrMatches(tb testing.TB, id string, err error, wantErr interface{}) {
	if err == nil {
		if wantErr == nil {
			return
		}

		if sub, ok := wantErr.(string); ok {
			tb.Errorf(red("%s | unexpected success; want error with substring %q"), id, sub)
			return
		}

		tb.Errorf(red("%s | unexpected success; want error %v"), id, wantErr)
		return
	}

	if wantErr == nil {
		tb.Errorf(red("%s | %v; want success"), id, err)
		return
	}

	if sub, ok := wantErr.(string); ok {
		if strings.Contains(err.Error(), sub) {
			return
		}
		tb.Errorf(red("%s | error = %v; want an error with substring %q"), id, err, sub)
		return
	}

	if err == wantErr {
		return
	}

	tb.Errorf(red("%s | %v; want %v"), id, err, wantErr)
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

func newTestClient(protocol, endpoint, connectionData string, ts *httptest.Server) (c *Client) {
	// Prepare a SignalR client.
	c = New(hostFromServerURL(ts.URL), protocol, endpoint, connectionData)
	c.HTTPClient = ts.Client()

	// Save the TLS config in case this is using TLS.
	if ts.TLS != nil {
		c.TLSClientConfig = ts.TLS
		c.Scheme = HTTPS
	} else {
		c.Scheme = HTTP
	}

	return
}

func negotiate(w http.ResponseWriter, r *http.Request) {
	_, err := w.Write([]byte(`{"ConnectionToken":"hello world","ConnectionId":"1234-ABC","URL":"/signalr","ProtocolVersion":"1337"}`))
	if err != nil {
		log.Panic(err)
	}
}

func connect(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Panic(err)
	}

	go func() {
		for {
			_, _, err = c.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	go func() {
		for {
			err = c.WriteMessage(websocket.TextMessage, []byte(`{"S":1}`))
			if err != nil {
				return
			}
		}
	}()
}

func reconnect(w http.ResponseWriter, r *http.Request) {
	connect(w, r)
}

func start(w http.ResponseWriter, r *http.Request) {
	_, err := w.Write([]byte(`{"Response":"started"}`))
	if err != nil {
		log.Panic(err)
	}
}

type fakeConn struct {
	errs chan error
}

func (c *fakeConn) ReadMessage() (messageType int, p []byte, err error) {
	err = <-c.errs
	return
}

func (c *fakeConn) WriteJSON(v interface{}) (err error) {
	return
}

func newFakeConn() *fakeConn {
	c := new(fakeConn)
	c.errs = make(chan error)
	return c
}

func panicErr(err error) {
	if err != nil {
		log.Panic(err)
	}
}

func TestClient_readMessages(t *testing.T) {
	cases := map[string]struct {
		inErrs  func() chan string
		wantErr interface{}
	}{
		"1000 error": {
			func() chan string {
				errCh := make(chan string)
				go func() {
					errCh <- "websocket: close 1000 (normal)"
					close(errCh)
				}()
				return errCh
			},
			nil,
		},
		"1001 error": {
			func() chan string {
				errCh := make(chan string)
				go func() {
					errCh <- "websocket: close 1001 (going away)"
					close(errCh)
				}()
				return errCh
			},
			nil,
		},
		"1006 error": {
			func() chan string {
				errCh := make(chan string)
				go func() {
					errCh <- "websocket: close 1006 (abnormal closure)"
					close(errCh)
				}()
				return errCh
			},
			nil,
		},
		"generic error": {
			func() chan string {
				errCh := make(chan string)
				go func() {
					errCh <- "generic error"
					close(errCh)
				}()
				return errCh
			},
			"generic error",
		},
		"1001, then 1006 error": {
			func() chan string {
				errCh := make(chan string)
				go func() {
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					close(errCh)
				}()
				return errCh
			},
			nil,
		},
		"1006, then 1001 error": {
			func() chan string {
				errCh := make(chan string)
				go func() {
					errCh <- "websocket: close 1006 (abnormal closure)"
					errCh <- "websocket: close 1001 (going away)"
					close(errCh)
				}()
				return errCh
			},
			nil,
		},
		"all the recoverable errors": {
			func() chan string {
				errCh := make(chan string)
				go func() {
					errCh <- "websocket: close 1000 (normal)"
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					errCh <- "websocket: close 1000 (normal)"
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					errCh <- "websocket: close 1000 (normal)"
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					errCh <- "websocket: close 1000 (normal)"
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					errCh <- "websocket: close 1000 (normal)"
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					errCh <- "websocket: close 1000 (normal)"
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					errCh <- "websocket: close 1000 (normal)"
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					close(errCh)
				}()
				return errCh
			},
			nil,
		},
		"multiple recoverable errors, followed by one unrecoverable error": {
			func() chan string {
				errCh := make(chan string)
				go func() {
					errCh <- "websocket: close 1000 (normal)"
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					errCh <- "websocket: close 1000 (normal)"
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					errCh <- "websocket: close 1000 (normal)"
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					errCh <- "websocket: close 1000 (normal)"
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					errCh <- "websocket: close 1000 (normal)"
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					errCh <- "websocket: close 1000 (normal)"
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					errCh <- "websocket: close 1000 (normal)"
					errCh <- "websocket: close 1001 (going away)"
					errCh <- "websocket: close 1006 (abnormal closure)"
					errCh <- "generic error"
					close(errCh)
				}()
				return errCh
			},
			"generic error",
		},
	}

	for id, tc := range cases {
		ts := newTestServer(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/negotiate") {
					negotiate(w, r)
				} else if strings.Contains(r.URL.Path, "/connect") {
					connect(w, r)
				} else if strings.Contains(r.URL.Path, "/reconnect") {
					reconnect(w, r)
				} else if strings.Contains(r.URL.Path, "/start") {
					start(w, r)
				} else {
					log.Println("url:", r.URL)
				}
			}), true)
		defer ts.Close()

		// Make a new client.
		c := newTestClient("1.5", "/signalr", "all the data", ts)
		c.RetryWaitDuration = 1 * time.Millisecond

		// Perform the first part of the initialization routine.
		var err error
		var conn WebsocketConn
		err = c.Negotiate()
		panicErr(err)
		conn, err = c.Connect()
		panicErr(err)
		err = c.Start(conn)
		panicErr(err)

		// Attach a test connection.
		fconn := newFakeConn()

		// Pipe errors to the connection.
		errsComplete := false
		ready := make(chan bool)
		go func() {
			errs := tc.inErrs()
			for tErr := range errs {
				fconn.errs <- errors.New(tErr)
			}
			go func() {
				errsComplete = true
				c.Close()
				ready <- true
			}()
		}()

		// Register the fake connection.
		c.Conn = fconn

		// Test readMessages.
		go c.readMessages()
		errs := c.Errors()
		msgs := c.Messages()
		go func() {
			// Save the error that is received.
			err = <-errs

			// Indicate we are ready to proceed.
			ready <- true
		}()
		go func() {
			for {
				// Receive a message.
				<-msgs

				// Reset the connection so it fails again. This
				// is the key to the whole test. We are
				// simulating as lots of combinations of
				// consecutive failures.
				c.Conn = fconn

				// Close the client.
				//log.Println("gothere6")
				if errsComplete {
					go func() {
						c.Close()

						// Indicate we are ready to
						// proceed. This happens if an
						// error was handled gracefully.
						ready <- true
					}()
				}
			}
		}()

		// Wait for the error to be returned.
		<-ready

		// Verify the results.
		testErrMatches(t, id, err, tc.wantErr)
	}
}
