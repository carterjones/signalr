package signalr

import (
	"bytes"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

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

func newTestServer(fn http.HandlerFunc, tls bool) *httptest.Server {
	var ts *httptest.Server

	if tls {
		// Create the server.
		ts = httptest.NewTLSServer(fn)

		// Save the testing certificate to the TLS client config.
		//
		// I'm not sure why ts.TLS doesn't contain certificate information.
		// However, this seems to make the testing TLS certificate be trusted by
		// the client.
		ts.TLS.RootCAs = x509.NewCertPool()
		ts.TLS.RootCAs.AddCert(ts.Certificate())
	} else {
		// Create the server.
		ts = httptest.NewServer(fn)
	}

	return ts
}

func newTestClient(protocol, endpoint, connectionData string, params map[string]string, ts *httptest.Server) *Client {
	// Prepare a SignalR client.
	c := New(hostFromServerURL(ts.URL), protocol, endpoint, connectionData, params)
	c.HTTPClient = ts.Client()

	// Save the TLS config in case this is using TLS.
	if ts.TLS != nil {
		c.TLSClientConfig = ts.TLS
		c.Scheme = HTTPS
	} else {
		c.Scheme = HTTP
	}

	return c
}

func negotiate(w http.ResponseWriter, _ *http.Request) {
	// nolint:lll
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

func reconnect(w http.ResponseWriter, r *http.Request) {
	connect(w, r)
}

func start(w http.ResponseWriter, _ *http.Request) {
	_, err := w.Write([]byte(`{"Response":"started"}`))
	if err != nil {
		log.Panic(err)
	}
}

type fakeConn struct {
	err     error
	errs    chan error
	msgType int
	msg     string
}

func (c *fakeConn) ReadMessage() (int, []byte, error) {
	// Set the message type.
	msgType := c.msgType

	// Set the message.
	p := []byte(c.msg)

	// Default to using the errs channel.
	if c.errs != nil {
		return 0, nil, <-c.errs
	}

	// Otherwise use a static error.
	err := c.err

	return msgType, p, err
}

func (c *fakeConn) WriteJSON(v interface{}) (err error) {
	return
}

func newFakeConn() *fakeConn {
	c := new(fakeConn)
	c.errs = make(chan error)
	c.msgType = websocket.TextMessage
	return c
}

func panicErr(err error) {
	if err != nil {
		log.Panic(err)
	}
}

// Use a custom log function so that they can be enabled only when an
// environment variable is set.
func logEvent(section, id, msg string, logs *string, logsMux sync.Locker) {
	logEvents := os.Getenv("LOG_EVENTS")
	if logEvents != "" {
		logsMux.Lock()
		*logs += fmt.Sprintf("[%s | %s] %s\n", section, id, msg)
		logsMux.Unlock()
	}
}

func TestClient_ReadMessages(t *testing.T) { // nolint: gocyclo
	t.Parallel()

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
		"many generic errors": {
			func() chan string {
				errCh := make(chan string)
				go func() {
					for i := 0; i < 20; i++ {
						errCh <- "generic error"
					}
					close(errCh)
				}()
				return errCh
			},
			"generic error",
		},
		"wait, then throw generic error": {
			func() chan string {
				errCh := make(chan string)
				go func() {
					time.Sleep(5 * time.Second)
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
					for i := 0; i < 5; i++ {
						errCh <- "websocket: close 1000 (normal)"
						errCh <- "websocket: close 1001 (going away)"
						errCh <- "websocket: close 1006 (abnormal closure)"
					}
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
					for i := 0; i < 5; i++ {
						errCh <- "websocket: close 1000 (normal)"
						errCh <- "websocket: close 1001 (going away)"
						errCh <- "websocket: close 1006 (abnormal closure)"
					}
					errCh <- "generic error"
					close(errCh)
				}()
				return errCh
			},
			"generic error",
		},
	}

	// Create a variable to store the logs that we will print after the tests
	// complete.
	var logs = ""
	var logsMux = sync.Mutex{}

	for id, tc := range cases {
		ts := newTestServer(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/negotiate"):
					negotiate(w, r)
				case strings.Contains(r.URL.Path, "/connect"):
					connect(w, r)
				case strings.Contains(r.URL.Path, "/reconnect"):
					reconnect(w, r)
				case strings.Contains(r.URL.Path, "/start"):
					start(w, r)
				default:
					log.Println("url:", r.URL)
				}
			}), true)
		defer ts.Close()

		// Make a new client.
		c := newTestClient("1.5", "/signalr", "all the data", nil, ts)
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
		inErrs := tc.inErrs()

		var wg sync.WaitGroup
		wg.Add(2)
		go func(id string, inErrs chan string, wantErr interface{}) {
			for tErr := range inErrs {
				fconn.errs <- errors.New(tErr)
			}
			logEvent("writer", id, "finished sending errors", &logs, &logsMux)

			// If we don't expect any errors...
			if wantErr == nil {
				// Signal that the connection should close.
				c.Close()
				logEvent("writer", id, "signaled to close channel (nil error expected)", &logs, &logsMux)

				// Mark this goroutine as done.
				wg.Done()
				logEvent("writer", id, "signaled done (nil error expected)", &logs, &logsMux)
				return
			}
		}(id, inErrs, tc.wantErr)

		// Register the fake connection.
		c.SetConn(fconn)

		// Test readMessages.
		msgs := make(chan Message)
		errs := make(chan error)

		go func(id string) {
			// Define handlers.
			msgHandler := func(msg Message) { msgs <- msg }
			errHandler := func(err error) { errs <- err }

			// Process all messages. This will finish when the connection is
			// closed.
			c.ReadMessages(msgHandler, errHandler)
			logEvent("reader", id, "finished reading messages", &logs, &logsMux)

			// At this point, the connection has been closed and the done signal
			// can be sent.
			wg.Done()
			logEvent("reader", id, "signaled done", &logs, &logsMux)
		}(id)

		// Wait for both loops to be done. Then send the done signal.
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

	loop:
		for {
			select {
			case <-msgs:
				// Reset the connection so it fails again. This is the key to
				// the whole test. We are simulating as lots of combinations of
				// consecutive failures.
				go c.SetConn(fconn)
			case err = <-errs:
				logEvent("main  ", id, "err received. breaking.", &logs, &logsMux)
				break loop
			case <-done:
				logEvent("main  ", id, "done received. breaking.", &logs, &logsMux)
				break loop
			}
		}

		// Verify the results.
		testErrMatches(t, id, err, tc.wantErr)
	}

	// We print the accumulated logs because merely printing them to the screen
	// as they occur tends to affect the timing of these tests, which results in
	// hard to identify Heisenbugs.
	if logs != "" {
		fmt.Println(logs)
	}
}

func TestClient_ReadMessages_earlyClose(t *testing.T) {
	t.Parallel()

	msgHandler := func(msg Message) {}
	errHandler := func(err error) {}
	done := make(chan struct{})

	c := New("", "", "", "", map[string]string{})
	conn := newFakeConn()
	c.SetConn(conn)

	// Launch a goroutine that starts the message reading loop. Send a done
	// signal once the loop terminates.
	go func() {
		c.ReadMessages(msgHandler, errHandler)
		done <- struct{}{}
	}()

	// Close the loop prior to sending any messages.
	c.Close()

	// Define a maximum amount of time to wait for his test to complete.
	maxWaitTime := time.Second * 2
	select {
	case <-time.After(maxWaitTime):
		t.Errorf("ReadMessages_earlyClose: timeout while waiting for message loop to close")
	case <-done:
		// Don't do anything; the test was a success since it received the done
		// signal.
	}
}

func TestClient_ReadMessages_longReconnectAttempt(t *testing.T) {
	t.Parallel()

	msgHandler := func(msg Message) {}
	errHandler := func(err error) {}
	done := make(chan struct{})

	ts := newTestServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "/negotiate"):
				negotiate(w, r)
			case strings.Contains(r.URL.Path, "/connect"):
				connect(w, r)
			case strings.Contains(r.URL.Path, "/reconnect"):
				// Intentionally wait indefinitely on reconnect.
				select {}
			case strings.Contains(r.URL.Path, "/start"):
				start(w, r)
			default:
				log.Println("url:", r.URL)
			}
		}), true)
	defer ts.Close()

	c := New("", "", "", "", map[string]string{})
	conn := newFakeConn()
	c.SetConn(conn)

	// Launch a goroutine that starts the message reading loop. Send a done
	// signal once the loop terminates.
	go func() {
		c.ReadMessages(msgHandler, errHandler)
		done <- struct{}{}
	}()

	// Define a maximum reconnect attempt time for the client.
	c.MaxReconnectAttemptDuration = 50 * time.Millisecond

	// Define a maximum amount of time to wait for his test to complete, which
	// is much longer than the reconnect attempt.
	maxWaitTime := time.Second * 2

	// Send a test error that will eventually hang while being processed during
	// the reconnection.
	conn.errs <- errors.New("websocket: close 1006 (abnormal closure)")

	select {
	case <-time.After(maxWaitTime):
		t.Errorf("ReadMessages_longReconnectAttempt: timeout while waiting for message loop to close")
	case <-done:
		// Don't do anything; the test was a success since it received the done
		// signal.
	}
}

func TestPrefixedID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in  string
		exp string
	}{
		{"", ""},
		{"123", "[123] "},
		{"abc", "[abc] "},
	}

	for _, tc := range cases {
		act := prefixedID(tc.in)
		equals(t, tc.in, tc.exp, act)
	}
}

func TestPrepareRequest(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		url     string
		headers map[string]string
		req     *http.Request
		wantErr string
	}{
		"simple request with no headers": {
			url:     "http://example.org",
			headers: map[string]string{},
			req: &http.Request{
				Host:       "example.org",
				Method:     "GET",
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				URL: &url.URL{
					Scheme: "http",
					Host:   "example.org",
				},
				Header: http.Header{},
			},
			wantErr: "",
		},
		"complex request with headers": {
			url: "https://example.org/custom/path?param=123",
			headers: map[string]string{
				"header1": "value1",
				"header2": "value2a,value2b",
			},
			req: &http.Request{
				Host:       "example.org",
				Method:     "GET",
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				URL: &url.URL{
					Scheme:   "https",
					Host:     "example.org",
					Path:     "/custom/path",
					RawQuery: "param=123",
				},
				Header: http.Header{
					"Header1": []string{"value1"},
					"Header2": []string{"value2a,value2b"},
				},
			},
			wantErr: "",
		},
		"invalid URL": {
			url:     ":",
			headers: map[string]string{},
			req:     nil,
			wantErr: "missing protocol scheme",
		},
	}

	for id, tc := range cases {
		req, err := prepareRequest(tc.url, tc.headers)
		equals(t, id, tc.req, req)

		if tc.wantErr != "" {
			testErrMatches(t, id, err, tc.wantErr)
		}
	}
}

func TestProcessReadMessagesMessage(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		p       []byte
		expMsg  *Message
		wantErr string
	}{
		"empty message": {
			p:       []byte(""),
			expMsg:  nil,
			wantErr: "json unmarshal failed",
		},
		"bad json": {
			p:       []byte("{invalid json"),
			expMsg:  nil,
			wantErr: "json unmarshal failed",
		},
		"keepalive": {
			p:       []byte("{}"),
			expMsg:  nil,
			wantErr: "",
		},
		"normal message": {
			p:       []byte(`{"C":"test message"}`),
			expMsg:  &Message{C: "test message"},
			wantErr: "",
		},
		"groups token": {
			p:       []byte(`{"C":"test message","G":"custom-groups-token"}`),
			expMsg:  &Message{C: "test message", G: "custom-groups-token"},
			wantErr: "",
		},
	}

	for id, tc := range cases {
		// Make channels to receive the data.
		msgs := make(chan Message)
		errs := make(chan error)

		// Define handlers.
		msgHandler := func(msg Message) { msgs <- msg }
		errHandler := func(err error) { errs <- err }

		c := new(Client)

		// Process the message.
		go c.processReadMessagesMessage(tc.p, msgHandler, errHandler)

		// Evaluate the results.
		select {
		case msg := <-msgs:
			equals(t, id, *tc.expMsg, msg)
			equals(t, id, tc.expMsg.C, c.MessageID.Get())
		case err := <-errs:
			testErrMatches(t, id, err, tc.wantErr)
		case <-time.After(500 * time.Millisecond):
			if tc.expMsg == nil && tc.wantErr == "" {
				// We don't expect any response in this case, so
				// we simply break.
				break
			}

			// Otherwise, an logic flaw likely exists, so we flag
			// it as an error.
			t.Errorf("timeout while processing " + id)
		}
	}
}

type EmptyCookieJar struct{}

func (j EmptyCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {}

func (j EmptyCookieJar) Cookies(u *url.URL) []*http.Cookie {
	return make([]*http.Cookie, 0)
}

type FakeCookieJar struct {
	cookies map[string]string
}

func (j FakeCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {}

func (j FakeCookieJar) Cookies(u *url.URL) []*http.Cookie {
	cookies := make([]*http.Cookie, len(j.cookies))
	i := 0
	for k, v := range j.cookies {
		cookies[i] = &http.Cookie{
			Name:  k,
			Value: v,
		}
		i++
	}

	// Sort it so the results are consistent.
	sort.Slice(cookies, func(i, j int) bool {
		return strings.Compare(cookies[i].Name, cookies[j].Name) < 0
	})

	return cookies
}

func TestMakeHeader(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in  *Client
		exp http.Header
	}{
		"nil client": {
			in:  nil,
			exp: http.Header{},
		},
		"nil http client": {
			in:  &Client{HTTPClient: nil},
			exp: http.Header{},
		},
		"empty cookie jar": {
			in: &Client{HTTPClient: &http.Client{
				Jar: EmptyCookieJar{},
			}},
			exp: http.Header{},
		},
		"one cookie": {
			in: &Client{HTTPClient: &http.Client{
				Jar: FakeCookieJar{
					cookies: map[string]string{"key1": "value1"},
				},
			}},
			exp: http.Header{
				"Cookie": []string{"key1=value1"},
			},
		},
		"three cookies": {
			in: &Client{HTTPClient: &http.Client{
				Jar: FakeCookieJar{
					cookies: map[string]string{
						"key1": "value1",
						"key2": "value2",
						"key3": "value3",
					},
				},
			}},
			exp: http.Header{
				"Cookie": []string{
					"key1=value1; key2=value2; key3=value3",
				},
			},
		},
		"one custom header": {
			in: &Client{Headers: map[string]string{
				"custom1": "value1",
			}},
			exp: http.Header{
				"Custom1": []string{"value1"},
			},
		},
		"three custom headers": {
			in: &Client{Headers: map[string]string{
				"custom1": "value1",
				"custom2": "value2",
				"custom3": "value3",
			}},
			exp: http.Header{
				"Custom1": []string{"value1"},
				"Custom2": []string{"value2"},
				"Custom3": []string{"value3"},
			},
		},
	}

	for id, tc := range cases {
		act := makeHeader(tc.in)

		equals(t, id, tc.exp, act)
	}
}

type fakeReadCloser struct {
	*bytes.Buffer
	rerr error
	cerr error
}

func (rc fakeReadCloser) Read(p []byte) (int, error) {
	if rc.rerr != nil {
		// Return a custom error for testing.
		return 0, rc.rerr
	}

	// Return the custom data for testing. Ignore the data sent to this
	// function.
	return rc.Buffer.Read(p)
}

func (rc fakeReadCloser) Close() error {
	return rc.cerr
}

func TestProcessStartResponse(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body    io.ReadCloser
		conn    WebsocketConn
		wantErr string
	}{
		"read failure": {
			body: &fakeReadCloser{
				rerr: errors.New("fake read error"),
			},
			conn:    &fakeConn{},
			wantErr: "read failed: fake read error",
		},
		"deferred close failure after normal return": {
			body: fakeReadCloser{
				Buffer: bytes.NewBufferString(`{"Response":"started"}`),
				cerr:   errors.New("fake close error"),
			},
			conn:    &fakeConn{msgType: 1, msg: `{"S":1}`},
			wantErr: "error in defer: fake close error",
		},
		"deferred close failure after read failure": {
			body: &fakeReadCloser{
				rerr: errors.New("fake read error"),
				cerr: errors.New("fake close error"),
			},
			conn:    &fakeConn{},
			wantErr: "fake close error: error in defer: read failed: fake read error",
		},
		"invalid json in response": {
			body:    fakeReadCloser{Buffer: bytes.NewBufferString("invalid json")},
			conn:    &fakeConn{},
			wantErr: "json unmarshal failed: invalid character 'i' looking for beginning of value",
		},
		"non-started response 1": {
			body:    fakeReadCloser{Buffer: bytes.NewBufferString(`{"hello":"world"}`)},
			conn:    &fakeConn{},
			wantErr: `start response is not 'started'`,
		},
		"non-stared response 2": {
			body:    fakeReadCloser{Buffer: bytes.NewBufferString(`{"Response":"blabla"}`)},
			conn:    &fakeConn{},
			wantErr: `start response is not 'started'`,
		},
		"readmessage failure": {
			body:    fakeReadCloser{Buffer: bytes.NewBufferString(`{"Response":"started"}`)},
			conn:    &fakeConn{err: errors.New("fake read error")},
			wantErr: "message read failed: fake read error",
		},
		"wrong message type": {
			body:    fakeReadCloser{Buffer: bytes.NewBufferString(`{"Response":"started"}`)},
			conn:    &fakeConn{msgType: 9001},
			wantErr: "unexpected websocket control type: 9001",
		},
		"message json unmarshal failure": {
			body:    fakeReadCloser{Buffer: bytes.NewBufferString(`{"Response":"started"}`)},
			conn:    &fakeConn{msgType: 1, msg: "invalid json"},
			wantErr: "json unmarshal failed: invalid character 'i' looking for beginning of value",
		},
		"server not initialized": {
			body:    fakeReadCloser{Buffer: bytes.NewBufferString(`{"Response":"started"}`)},
			conn:    &fakeConn{msgType: 1, msg: `{"S":9002}`},
			wantErr: `unexpected S value received from server: 9002 | message: {"S":9002}`,
		},
		"successful call": {
			body:    fakeReadCloser{Buffer: bytes.NewBufferString(`{"Response":"started"}`)},
			conn:    &fakeConn{msgType: 1, msg: `{"S":1}`},
			wantErr: "",
		},
	}

	for id, tc := range cases {
		// Make a new client.
		c := New("", "", "", "", nil)

		err := c.processStartResponse(tc.body, tc.conn)

		if tc.wantErr != "" {
			testErrMatches(t, id, err, tc.wantErr)
		} else {
			equals(t, id, tc.conn, c.conn)
		}
	}
}

func TestProcessNegotiateResponse(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body            io.ReadCloser
		connectionToken string
		connectionID    string
		protocol        string
		endpoint        string
		wantErr         string
	}{
		"read failure": {
			body:    fakeReadCloser{rerr: errors.New("fake read error")},
			wantErr: "read failed: fake read error",
		},
		"deferred close failure after normal return": {
			body: fakeReadCloser{
				// nolint:lll
				Buffer: bytes.NewBufferString(`{"ConnectionToken":"123abc","ConnectionID":"456def","ProtocolVersion":"my-custom-protocol","Url":"super-awesome-signalr"}`),
				cerr:   errors.New("fake close error"),
			},
			wantErr: "error in defer: fake close error",
		},
		"deferred close failure after read error": {
			body: &fakeReadCloser{
				rerr: errors.New("fake read error"),
				cerr: errors.New("fake close error"),
			},
			wantErr: "fake close error: error in defer: read failed: fake read error",
		},
		"empty json": {
			body:    fakeReadCloser{Buffer: bytes.NewBufferString("")},
			wantErr: "json unmarshal failed: unexpected end of JSON input",
		},
		"invalid json": {
			body:    fakeReadCloser{Buffer: bytes.NewBufferString("invalid json")},
			wantErr: "json unmarshal failed: invalid character 'i' looking for beginning of value",
		},
		"valid data": {
			// nolint:lll
			body:            fakeReadCloser{Buffer: bytes.NewBufferString(`{"ConnectionToken":"123abc","ConnectionID":"456def","ProtocolVersion":"my-custom-protocol","Url":"super-awesome-signalr"}`)},
			connectionToken: "123abc",
			connectionID:    "456def",
			protocol:        "my-custom-protocol",
			endpoint:        "super-awesome-signalr",
			wantErr:         "",
		},
	}

	for id, tc := range cases {
		// Create a test client.
		c := New("", "", "", "", nil)

		// Get the result.
		err := c.processNegotiateResponse(tc.body)

		// Evaluate the result.
		if tc.wantErr != "" {
			testErrMatches(t, id, err, tc.wantErr)
		} else {
			equals(t, id, tc.connectionToken, c.ConnectionToken)
			equals(t, id, tc.connectionID, c.ConnectionID)
			equals(t, id, tc.protocol, c.Protocol)
			equals(t, id, tc.endpoint, c.Endpoint)
		}
	}
}

func TestClient_attemptReconnect(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		maxRetries int
	}{
		"successful reconnect": {
			maxRetries: 5,
		},
		"unsuccessful reconnect": {
			maxRetries: 0,
		},
	}

	for _, tc := range cases {
		// Create a test client.
		c := New("", "", "", "", nil)

		// Set the maximum number of retries.
		c.MaxReconnectRetries = tc.maxRetries
		c.RetryWaitDuration = 1 * time.Millisecond

		// Attempt to reconnect.
		c.attemptReconnect()
	}
}
