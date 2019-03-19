package signalr_test

import (
	"crypto/tls"
	"errors"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/carterjones/signalr"
	"github.com/carterjones/signalr/hubs"
	"github.com/gorilla/websocket"
)

func ExampleClient_Run() {
	// Prepare a SignalR client.
	c := signalr.New(
		"fake-server.definitely-not-real",
		"1.5",
		"/signalr",
		`[{"name":"awesomehub"}]`,
		nil,
	)

	// Define handlers.
	msgHandler := func(msg signalr.Message) { log.Println(msg) }
	panicIfErr := func(err error) {
		if err != nil {
			log.Panic(err)
		}
	}

	// Start the connection.
	err := c.Run(msgHandler, panicIfErr)
	if err != nil {
		log.Panic(err)
	}

	// Wait indefinitely.
	select {}
}

// This example shows the most basic way to start a websocket connection.
func Example_basic() {
	// Prepare a SignalR client.
	c := signalr.New(
		"fake-server.definitely-not-real",
		"1.5",
		"/signalr",
		`[{"name":"awesomehub"}]`,
		nil,
	)

	// Define message and error handlers.
	msgHandler := func(msg signalr.Message) { log.Println(msg) }
	panicIfErr := func(err error) {
		if err != nil {
			log.Panic(err)
		}
	}

	// Start the connection.
	err := c.Run(msgHandler, panicIfErr)
	panicIfErr(err)

	// Wait indefinitely.
	select {}
}

// This example shows how to manually perform each of the initialization steps.
func Example_complex() {
	// Prepare a SignalR client.
	c := signalr.New(
		"fake-server.definitely-not-real",
		"1.5",
		"/signalr",
		`[{"name":"awesomehub"}]`,
		map[string]string{"custom-key": "custom-value"},
	)

	// Perform any optional modifications to the client here. Read the docs for
	// all the available options that are exposed via public fields.

	// Define message and error handlers.
	msgHandler := func(msg signalr.Message) { log.Println(msg) }
	panicIfErr := func(err error) {
		if err != nil {
			log.Panic(err)
		}
	}

	// Manually perform the initialization routine.
	err := c.Negotiate()
	panicIfErr(err)
	conn, err := c.Connect()
	panicIfErr(err)
	err = c.Start(conn)
	panicIfErr(err)

	// Begin the message reading loop.
	go c.ReadMessages(msgHandler, panicIfErr)

	// Wait indefinitely.
	select {}
}

func red(s string) string {
	return "\033[31m" + s + "\033[39m"
}

func equals(tb testing.TB, id string, exp, act interface{}) {
	if !reflect.DeepEqual(exp, act) {
		_, file, line, _ := runtime.Caller(1)
		tb.Errorf(red("%s:%d %s: \n\texp: %#v\n\tgot: %#v\n"),
			filepath.Base(file), line, id, exp, act)
	}
}

func ok(tb testing.TB, id string, err error) {
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		tb.Errorf(red("%s:%d %s | unexpected error: %s\n"),
			filepath.Base(file), line, id, err.Error())
	}
}

func notNil(tb testing.TB, id string, act interface{}) {
	if act == nil {
		_, file, line, _ := runtime.Caller(1)
		tb.Errorf(red("%s:%d (%s):\n\texp: a non-nil value\n\tgot: %#v\n"),
			filepath.Base(file), line, id, act)
	}
}

func notEmpty(tb testing.TB, id string, act string) {
	if act == "" {
		_, file, line, _ := runtime.Caller(1)
		tb.Errorf(red("%s:%d (%s):\n\texp: a non-empty value\n\tgot: %#v\n"),
			filepath.Base(file), line, id, act)
	}
}

// Note: this is largely derived from
// https://github.com/golang/go/blob/1c69384da4fb4a1323e011941c101189247fea67/src/net/http/response_test.go#L915-L940
func errMatches(tb testing.TB, id string, err error, wantErr interface{}) {
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

const (
	serverResponseWriteTimeout = 500 * time.Millisecond
)

func newTestServer(fn http.HandlerFunc, useTLS bool) *httptest.Server {
	// Create the server.
	ts := httptest.NewUnstartedServer(fn)

	// Set the write timeout so that we can test timeouts later on.
	ts.Config.WriteTimeout = serverResponseWriteTimeout

	if useTLS {
		ts.StartTLS()
	} else {
		ts.Start()
	}

	return ts
}

func newTestClient(
	protocol, endpoint, connectionData string,
	params map[string]string,
	ts *httptest.Server,
) *signalr.Client {
	// Prepare a SignalR client.
	c := signalr.New(hostFromServerURL(ts.URL), protocol, endpoint, connectionData, params)
	c.HTTPClient = ts.Client()

	// Save the TLS config in case this is using TLS.
	if ts.TLS != nil {
		// This is a local-only test, so we don't care about validating
		// certificates. This simplifies things greatly.
		// nolint:gosec
		c.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		c.Scheme = signalr.HTTPS
	} else {
		c.Scheme = signalr.HTTP
	}

	return c
}

func throw503Error(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusServiceUnavailable)
	_, err := w.Write([]byte("503 error"))
	if err != nil {
		log.Panic(err)
	}
}

func throw678Error(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(678)
	_, err := w.Write([]byte("678 error"))
	if err != nil {
		log.Panic(err)
	}
}

func throw404Error(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	_, err := w.Write([]byte("404 error"))
	if err != nil {
		log.Panic(err)
	}
}

func causeWriteResponseTimeout(w http.ResponseWriter, r *http.Request) {
	time.Sleep(3 * serverResponseWriteTimeout)
}

func TestClient_Negotiate(t *testing.T) {
	t.Parallel()

	// Make a requestID available to test cases in the event that multiple
	// requests are sent that should have different responses based on which
	// request is being sent.
	var requestID int
	log.Println(requestID)

	cases := map[string]struct {
		fn       http.HandlerFunc
		in       *signalr.Client
		TLS      bool
		useDebug bool
		exp      *signalr.Client
		scheme   signalr.Scheme
		params   map[string]string
		wantErr  string
	}{
		"successful http": {
			fn: signalr.TestNegotiate,
			in: &signalr.Client{
				Protocol:       "1337",
				Endpoint:       "/signalr",
				ConnectionData: "all the data",
			},
			TLS: false,
			exp: &signalr.Client{
				Protocol:        "1337",
				Endpoint:        "/signalr",
				ConnectionToken: "hello world",
				ConnectionID:    "1234-ABC",
				ConnectionData:  "",
			},
		},
		"successful https": {
			fn: signalr.TestNegotiate,
			in: &signalr.Client{
				Protocol:       "1337",
				Endpoint:       "/signalr",
				ConnectionData: "all the data",
			},
			TLS: true,
			exp: &signalr.Client{
				Protocol:        "1337",
				Endpoint:        "/signalr",
				ConnectionToken: "hello world",
				ConnectionID:    "1234-ABC",
			},
		},
		"503 error": {
			fn:      throw503Error,
			in:      &signalr.Client{},
			exp:     &signalr.Client{},
			wantErr: "503 Service Unavailable",
		},
		"default error": {
			fn:      throw678Error,
			in:      &signalr.Client{},
			exp:     &signalr.Client{},
			wantErr: "678 status code",
		},
		"failed get request": {
			fn:      causeWriteResponseTimeout,
			in:      &signalr.Client{},
			exp:     &signalr.Client{},
			wantErr: "EOF",
		},
		"invalid json": {
			fn: func(w http.ResponseWriter, r *http.Request) {
				_, err := w.Write([]byte("invalid json"))
				if err != nil {
					log.Panic(err)
				}
			},
			in:      &signalr.Client{},
			exp:     &signalr.Client{},
			wantErr: "invalid character 'i' looking for beginning of value",
		},
		"request preparation failure": {
			fn:      signalr.TestNegotiate,
			in:      &signalr.Client{},
			scheme:  ":",
			exp:     &signalr.Client{},
			wantErr: "request preparation failed",
		},
		"call debug messages": {
			fn:       throw503Error,
			in:       &signalr.Client{},
			exp:      &signalr.Client{},
			useDebug: true,
			wantErr:  "503 Service Unavailable",
		},
		"recover after failure": {
			fn: func(w http.ResponseWriter, r *http.Request) {
				if requestID == 0 {
					throw503Error(w, r)
					requestID++
				} else {
					signalr.TestNegotiate(w, r)
				}
			},
			in: &signalr.Client{
				Protocol:       "1337",
				Endpoint:       "/signalr",
				ConnectionData: "all the data",
			},
			TLS: false,
			exp: &signalr.Client{
				Protocol:        "1337",
				Endpoint:        "/signalr",
				ConnectionToken: "hello world",
				ConnectionID:    "1234-ABC",
				ConnectionData:  "",
			},
		},
		"custom parameters": {
			fn: signalr.TestNegotiate,
			in: &signalr.Client{
				Protocol:       "1337",
				Endpoint:       "/signalr",
				ConnectionData: "all the data",
			},
			TLS:    false,
			params: map[string]string{"custom-key": "custom-value"},
			exp: &signalr.Client{
				Protocol:        "1337",
				Endpoint:        "/signalr",
				ConnectionToken: "hello world",
				ConnectionID:    "1234-ABC",
				ConnectionData:  "",
			},
		},
	}

	for id, tc := range cases {
		tc := tc

		// Set the debug flag.
		if tc.useDebug {
			os.Setenv("DEBUG", "true")
		}

		// Reset the request ID.
		requestID = 0

		// Prepare to save parameters.
		var params map[string]string
		done := make(chan struct{})

		// Create a test server.
		ts := newTestServer(func(w http.ResponseWriter, r *http.Request) {
			params = extractCustomParams(r.URL.Query())
			tc.fn(w, r)
			go func() { done <- struct{}{} }()
		}, tc.TLS)

		// Create a test client.
		c := newTestClient(tc.in.Protocol, tc.in.Endpoint, tc.in.ConnectionData, tc.params, ts)

		// Set the wait time to milliseconds.
		c.RetryWaitDuration = 1 * time.Millisecond

		// Set a custom scheme if one is specified.
		if tc.scheme != "" {
			c.Scheme = tc.scheme
		}

		// Perform the negotiation.
		err := c.Negotiate()

		// If the scheme is invalid, this will never send a request, so we move
		// on. Otherwise, we wait for the request to complete.
		if tc.scheme != ":" {
			<-done
		}

		// Make sure the error matches the expected error.
		if tc.wantErr != "" {
			errMatches(t, id, err, tc.wantErr)
		} else {
			ok(t, id, err)
		}

		// Validate the things we expect.
		equals(t, id, tc.exp.ConnectionToken, c.ConnectionToken)
		equals(t, id, tc.exp.ConnectionID, c.ConnectionID)
		equals(t, id, tc.exp.Protocol, c.Protocol)
		equals(t, id, tc.exp.Endpoint, c.Endpoint)
		equals(t, id, tc.params, params)

		ts.Close()

		// Unset the debug flag.
		if tc.useDebug {
			os.Unsetenv("DEBUG")
		}
	}
}

func extractCustomParams(values url.Values) map[string]string {
	// Remove the parameters that we know will be there.
	values.Del("transport")
	values.Del("clientProtocol")
	values.Del("connectionData")
	values.Del("tid")

	// Return nil if nothing remains.
	if len(values) == 0 {
		return nil
	}

	// Save the custom parameters.
	params := make(map[string]string)
	for k, v := range values {
		params[k] = v[0]
	}

	// Return the custom parameters.
	return params
}

func TestClient_Connect(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		fn      http.HandlerFunc
		TLS     bool
		cookies []*http.Cookie
		params  map[string]string
		wantErr string
	}{
		"successful https connect": {
			fn:  signalr.TestConnect,
			TLS: true,
		},
		"successful http connect": {
			fn:  signalr.TestConnect,
			TLS: false,
		},
		"service not available": {
			fn:      throw503Error,
			TLS:     true,
			wantErr: websocket.ErrBadHandshake.Error(),
		},
		"generic error": {
			fn:      throw404Error,
			TLS:     true,
			wantErr: "xconnect failed: 404 Not Found, retry 0: websocket: bad handshake",
		},
		"custom cookie jar": {
			fn:  signalr.TestConnect,
			TLS: false,
			cookies: []*http.Cookie{{
				Name:  "hello",
				Value: "world",
			}},
		},
		"custom parameters": {
			fn:     signalr.TestConnect,
			TLS:    true,
			params: map[string]string{"custom-key": "custom-value"},
		},
	}

	for id, tc := range cases {
		tc := tc

		// Make a cookie recording wrapper function.
		done := make(chan struct{})
		var cookies []*http.Cookie
		var params map[string]string
		var tid string
		recordResponse := func(w http.ResponseWriter, r *http.Request) {
			cookies = r.Cookies()
			params = extractCustomParams(r.URL.Query())
			tid = r.URL.Query().Get("tid")
			tc.fn(w, r)
			go func() { done <- struct{}{} }()
		}

		// Set up the test server.
		ts := newTestServer(recordResponse, tc.TLS)

		// Prepare a new client.
		c := newTestClient("", "", "", tc.params, ts)

		// Set cookies if they have been configured.
		if tc.cookies != nil {
			u, err := url.Parse(ts.URL)
			if err != nil {
				log.Panic(err)
			}
			c.HTTPClient.Jar, err = cookiejar.New(nil)
			if err != nil {
				log.Panic(err)
			}
			c.HTTPClient.Jar.SetCookies(u, tc.cookies)
		}

		// Set the wait time to milliseconds.
		c.RetryWaitDuration = 1 * time.Millisecond

		// Perform the connection.
		conn, err := c.Connect()
		<-done

		if tc.wantErr != "" {
			errMatches(t, id, err, tc.wantErr)
		} else {
			if len(tc.cookies) > 0 {
				equals(t, id, tc.cookies, cookies)
			}
			equals(t, id, tc.params, params)
			ok(t, id, err)
			notEmpty(t, id, tid)
			_, cerr := strconv.Atoi(tid)
			ok(t, id, cerr)
		}

		notNil(t, id, conn)

		ts.Close()
	}
}

func TestClient_Reconnect(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		fn          http.HandlerFunc
		groupsToken string
		messageID   string
		wantErr     string
	}{
		"successful reconnect": {
			fn: signalr.TestReconnect,
		},
		"groups token": {
			fn:          signalr.TestReconnect,
			groupsToken: "my-custom-token",
		},
		"message id": {
			fn:        signalr.TestReconnect,
			messageID: "unique-message-id",
		},
	}

	for id, tc := range cases {
		tc := tc

		// Make a cookie recording wrapper function.
		done := make(chan struct{})
		var groupsToken string
		var messageID string
		recordResponse := func(w http.ResponseWriter, r *http.Request) {
			groupsToken = r.URL.Query().Get("groupsToken")
			messageID = r.URL.Query().Get("messageId")
			tc.fn(w, r)
			go func() { done <- struct{}{} }()
		}

		// Set up the test server.
		ts := newTestServer(recordResponse, true)

		// Prepare a new client.
		c := newTestClient("", "", "", nil, ts)

		// Set the wait time to milliseconds.
		c.RetryWaitDuration = 1 * time.Millisecond

		// Set the group token.
		c.GroupsToken.Set(tc.groupsToken)
		c.MessageID.Set(tc.messageID)

		// Perform the connection.
		conn, err := c.Reconnect()
		<-done

		if tc.wantErr != "" {
			errMatches(t, id, err, tc.wantErr)
		} else {
			ok(t, id, err)
			equals(t, id, tc.groupsToken, groupsToken)
			equals(t, id, tc.messageID, messageID)
		}

		notNil(t, id, conn)

		ts.Close()
	}
}

func handleWebsocketWithCustomMsg(w http.ResponseWriter, r *http.Request, msg string) {
	upgrader := websocket.Upgrader{}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Panic(err)
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
			werr := c.WriteMessage(websocket.TextMessage, []byte(msg))
			if werr != nil {
				return
			}
		}
	}()
}

func TestClient_Start(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		skipConnect bool
		skipRetries bool
		startFn     http.HandlerFunc
		connectFn   http.HandlerFunc
		scheme      signalr.Scheme
		params      map[string]string
		groupsToken string
		messageID   string
		wantErr     string
	}{
		"successful start": {
			startFn:   signalr.TestStart,
			connectFn: signalr.TestConnect,
		},
		"nil connection": {
			skipConnect: true,
			wantErr:     "connection is nil",
		},
		"failed get request": {
			startFn:   causeWriteResponseTimeout,
			connectFn: signalr.TestConnect,
			wantErr:   "EOF",
		},
		"invalid json sent in response to get request": {
			startFn: func(w http.ResponseWriter, r *http.Request) {
				_, err := w.Write([]byte("invalid json"))
				if err != nil {
					log.Panic(err)
				}
			},
			connectFn: signalr.TestConnect,
			wantErr:   "invalid character 'i' looking for beginning of value",
		},
		"non-'started' response": {
			startFn: func(w http.ResponseWriter, r *http.Request) {
				_, err := w.Write([]byte(`{"Response":"not expecting this"}`))
				if err != nil {
					log.Panic(err)
				}
			},
			connectFn: signalr.TestConnect,
			wantErr:   "start response is not 'started': not expecting this",
		},
		"non-text message from websocket": {
			startFn: signalr.TestStart,
			connectFn: func(w http.ResponseWriter, r *http.Request) {
				upgrader := websocket.Upgrader{}
				c, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					log.Panic(err)
				}
				err = c.WriteMessage(websocket.BinaryMessage, []byte("non-text message"))
				if err != nil {
					log.Panic(err)
				}
			},
			wantErr: "unexpected websocket control type",
		},
		"invalid json sent in init message": {
			startFn: signalr.TestStart,
			connectFn: func(w http.ResponseWriter, r *http.Request) {
				upgrader := websocket.Upgrader{}
				c, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					log.Panic(err)
				}
				err = c.WriteMessage(websocket.TextMessage, []byte("invalid json"))
				if err != nil {
					log.Panic(err)
				}
			},
			wantErr: "invalid character 'i' looking for beginning of value",
		},
		"wrong S value from server": {
			startFn: signalr.TestStart,
			connectFn: func(w http.ResponseWriter, r *http.Request) {
				upgrader := websocket.Upgrader{}
				c, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					log.Panic(err)
				}
				err = c.WriteMessage(websocket.TextMessage, []byte(`{"S":3}`))
				if err != nil {
					log.Panic(err)
				}
			},
			wantErr: "unexpected S value received from server",
		},
		"request preparation failure": {
			startFn:   signalr.TestStart,
			connectFn: signalr.TestConnect,
			scheme:    ":",
			wantErr:   "request preparation failed",
		},
		"empty response": {
			skipRetries: true,
			startFn:     signalr.TestStart,
			connectFn:   signalr.TestConnect,
			wantErr:     "response is nil",
		},
		"custom parameters": {
			startFn:   signalr.TestStart,
			connectFn: signalr.TestConnect,
			params:    map[string]string{"custom-key": "custom-value"},
		},
		"groups token": {
			startFn:     signalr.TestStart,
			groupsToken: "my-custom-groups-token",
			connectFn: func(w http.ResponseWriter, r *http.Request) {
				handleWebsocketWithCustomMsg(w, r, `{"S":1,"G":"my-custom-groups-token"}`)
			},
		},
		"message id": {
			startFn:   signalr.TestStart,
			messageID: "my-custom-message-id",
			connectFn: func(w http.ResponseWriter, r *http.Request) {
				handleWebsocketWithCustomMsg(w, r, `{"S":1,"C":"my-custom-message-id"}`)
			},
		},
	}

	for id, tc := range cases {
		tc := tc
		var params map[string]string

		// Create a test server that is initialized with this test
		// case's "start handler".
		ts := newTestServer(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/start") {
				params = extractCustomParams(r.URL.Query())
				tc.startFn(w, r)
			} else if strings.Contains(r.URL.Path, "/connect") {
				tc.connectFn(w, r)
			}
		}, true)

		// Create a test client and establish the initial connection.
		c := newTestClient("", "", "", tc.params, ts)

		// Set the wait time to milliseconds.
		c.RetryWaitDuration = 1 * time.Millisecond

		// Don't perform any retries.
		if tc.skipRetries {
			c.MaxStartRetries = 0
		}

		// Perform the connection.
		var conn signalr.WebsocketConn
		var err error
		if !tc.skipConnect {
			conn, err = c.Connect()
			if err != nil {
				// If this fails, it is not part of the test, so we
				// panic here.
				log.Panic(err)
			}
		}

		// Set a custom scheme if one is specified.
		if tc.scheme != "" {
			c.Scheme = tc.scheme
		}

		// Execute the start function.
		err = c.Start(conn)
		if tc.wantErr != "" {
			errMatches(t, id, err, tc.wantErr)
		} else {
			// Verify that the connection was properly set.
			equals(t, id, conn, c.Conn())

			// Verify no error occurred.
			ok(t, id, err)

			// Verify parameters were properly set.
			equals(t, id, tc.params, params)

			// Verify the groups token was properly set.
			equals(t, id, tc.groupsToken, c.GroupsToken.Get())

			// Verify the message ID was properly set.
			equals(t, id, tc.messageID, c.MessageID.Get())
		}

		ts.Close()
	}
}

func TestClient_Init(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		negotiateFn func(http.ResponseWriter, *http.Request)
		connectFn   func(http.ResponseWriter, *http.Request)
		startFn     func(http.ResponseWriter, *http.Request)
		wantErr     string
	}{
		"successful init": {
			negotiateFn: signalr.TestNegotiate,
			connectFn:   signalr.TestConnect,
			startFn:     signalr.TestStart,
			wantErr:     "",
		},
		"failed negotiate": {
			negotiateFn: func(w http.ResponseWriter, r *http.Request) {
				_, err := w.Write([]byte("invalid json"))
				if err != nil {
					log.Panic(err)
				}
			},
			wantErr: "json unmarshal failed: invalid character 'i' looking for beginning of value",
		},
		"failed connect": {
			negotiateFn: signalr.TestNegotiate,
			connectFn:   throw678Error,
			wantErr:     "connect failed: xconnect failed: 678 status code 678",
		},
		"failed start": {
			negotiateFn: signalr.TestNegotiate,
			connectFn:   signalr.TestConnect,
			startFn:     causeWriteResponseTimeout,
			wantErr:     "EOF",
		},
	}

	for id, tc := range cases {
		tc := tc

		ts := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "/negotiate"):
				tc.negotiateFn(w, r)
			case strings.Contains(r.URL.Path, "/connect"):
				tc.connectFn(w, r)
			case strings.Contains(r.URL.Path, "/start"):
				tc.startFn(w, r)
			default:
				log.Println("url:", r.URL)
			}
		}), true)

		c := newTestClient("1.5", "/signalr", "all the data", nil, ts)
		c.RetryWaitDuration = 1 * time.Millisecond

		// Define handlers.
		msgHandler := func(signalr.Message) {}
		errHandler := func(error) {}

		// Run the client.
		err := c.Run(msgHandler, errHandler)

		if tc.wantErr != "" {
			errMatches(t, id, err, tc.wantErr)
		} else {
			ok(t, id, err)
		}

		ts.Close()
	}
}

type FakeConn struct {
	err  error
	data interface{}
}

func (c *FakeConn) ReadMessage() (messageType int, p []byte, err error) {
	return
}

func (c *FakeConn) WriteJSON(v interface{}) error {
	// Save the data that is supposedly being written, so it can be
	// inspected later.
	c.data = v

	return c.err
}

func TestClient_Send(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		conn    *FakeConn
		err     error
		wantErr string
	}{
		"successful write": {
			conn:    new(FakeConn),
			err:     nil,
			wantErr: "",
		},
		"connection not set": {
			conn:    nil,
			err:     nil,
			wantErr: "send: connection not set",
		},
		"write error": {
			conn:    new(FakeConn),
			err:     errors.New("test error"),
			wantErr: "test error",
		},
	}

	for id, tc := range cases {
		// Set up a new test client.
		c := signalr.New("", "", "", "", nil)

		// Set up a fake connection, if one has been created.
		if tc.conn != nil {
			tc.conn.err = tc.err
			c.SetConn(tc.conn)
		}

		// Send the message.
		data := hubs.ClientMsg{H: "test data 123"}
		err := c.Send(data)

		// Check the results.
		if tc.wantErr != "" {
			errMatches(t, id, err, tc.wantErr)
		} else {
			equals(t, id, data, tc.conn.data)
			ok(t, id, err)
		}
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	// Define parameter values.
	host := "test-host"
	protocol := "test-protocol"
	endpoint := "test-endpoint"
	connectionData := "test-connection-data"
	params := map[string]string{
		"test-key": "test-value",
	}

	// Create the client.
	c := signalr.New(host, protocol, endpoint, connectionData, params)

	// Validate values were set up properly.
	equals(t, "host", host, c.Host)
	equals(t, "protocol", protocol, c.Protocol)
	equals(t, "endpoint", endpoint, c.Endpoint)
	equals(t, "connection data", connectionData, c.ConnectionData)
	notNil(t, "http client", c.HTTPClient)
	notNil(t, "http client transport", c.HTTPClient.Transport)
	equals(t, "scheme", signalr.HTTPS, c.Scheme)
	equals(t, "max negotiate retries", 5, c.MaxNegotiateRetries)
	equals(t, "max connect retries", 5, c.MaxConnectRetries)
	equals(t, "max reconnect retries", 5, c.MaxReconnectRetries)
	equals(t, "max start retries", 5, c.MaxStartRetries)
	equals(t, "retry wait duration", 1*time.Minute, c.RetryWaitDuration)
}
