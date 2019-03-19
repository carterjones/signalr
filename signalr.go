package signalr

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	scraper "github.com/carterjones/go-cloudflare-scraper"
	"github.com/carterjones/signalr/hubs"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

// Client represents a SignlR client. It manages connections so that the caller
// doesn't have to.
type Client struct {
	// The host providing the SignalR service.
	Host string

	// The relative path where the SignalR service is provided.
	Endpoint string

	// The websockets protocol version.
	Protocol string

	// Connection data passed to the service's websocket.
	ConnectionData string

	// User-defined custom parameters passed with each request to the server.
	Params map[string]string

	// The HTTPClient used to initialize the websocket connection.
	HTTPClient *http.Client

	// An optional setting to provide a non-default TLS configuration to use
	// when connecting to the websocket.
	TLSClientConfig *tls.Config

	// Either HTTPS or HTTP.
	Scheme Scheme

	// The maximum number of times to re-attempt a negotiation.
	MaxNegotiateRetries int

	// The maximum number of times to re-attempt a connection.
	MaxConnectRetries int

	// The maximum number of times to re-attempt a reconnection.
	MaxReconnectRetries int

	// The maximum number of times to re-attempt a start command.
	MaxStartRetries int

	// The time to wait before retrying, in the event that an error occurs
	// when contacting the SignalR service.
	RetryWaitDuration time.Duration

	// The maximum amount of time to spend retrying a reconnect attempt.
	MaxReconnectAttemptDuration time.Duration

	// This is the connection token set during the negotiate phase of the
	// protocol and used to uniquely identify the connection to the server
	// in all subsequent phases of the connection.
	ConnectionToken string

	// This is the ID of the connection. It is set during the negotiate
	// phase and then ignored by all subsequent steps.
	ConnectionID string

	// The groups token that is used during reconnect attempts.
	//
	// This is an example groups token:
	// nolint:lll
	// yUcSohHrAZGEwK62B4Ao0WYac82p5yeRvHHInBgVmSK7jX++ym3kIgDy466yW/gRPp2l3Py8G45mRLJ9FslB3sKfsDPUNWL1b54cvjaSXCUo0znzyACxrN2Y0kNLR59h7hb6PgOSfy3Z2R5CUSVm5LZg6jg=
	GroupsToken SafeString

	// The message ID that is used during reconnect attempts.
	//
	// This is an example message ID: d-8B839DC3-C,0|aaZe,0|aaZf,2|C1,2A801
	MessageID SafeString

	// Header values that should be applied to all HTTP requests.
	Headers map[string]string

	// This value is not part of the SignalR protocol. If this value is set,
	// it will be used in debug messages.
	CustomID string

	// This field holds a struct that can read messages from and write JSON
	// objects to a websocket. In practice, this is simply a raw websocket
	// connection that results from a successful connection to the SignalR
	// server.
	conn    WebsocketConn
	connMux sync.Mutex

	close chan struct{}
}

// Negotiate implements the negotiate step of the SignalR connection sequence.
func (c *Client) Negotiate() error {
	var err error

	// Reset the connection token in case it has been set.
	c.ConnectionToken = ""

	// Make a "negotiate" URL.
	u := makeURL("negotiate", c)

	// Make a flag to use for indicating whether or not an error occurred.
	errOccurred := false

	for i := 0; i < c.MaxNegotiateRetries; i++ {
		var req *http.Request
		req, err = prepareRequest(u.String(), c.Headers)
		if err != nil {
			return errors.Wrap(err, "request preparation failed")
		}

		// Perform the request.
		var resp *http.Response
		resp, err = c.HTTPClient.Do(req)
		if err != nil {
			return errors.Wrap(err, "request failed")
		}

		// Perform operations specific to the status code.
		switch resp.StatusCode {
		case 200:
			// Everything worked, so do nothing.
		case 503:
			fallthrough
		case 524:
			fallthrough
		default:
			err = errors.Errorf("request failed: %s", resp.Status)
			debugMessage("%snegotiate: retrying after %s", prefixedID(c.CustomID), resp.Status)
			errOccurred = true
			time.Sleep(c.RetryWaitDuration)
			continue
		}

		err = c.processNegotiateResponse(resp.Body)

		if errOccurred {
			// If an error occurred earlier, and yet we got here,
			// then we want to let the user know that the
			// negotiation successfully recovered.
			debugMessage("%sthe negotiate retry was successful", prefixedID(c.CustomID))
		}

		return err
	}

	if errOccurred {
		debugMessage("%sthe negotiate retry was unsuccessful", prefixedID(c.CustomID))
	}

	return err
}

func (c *Client) processNegotiateResponse(body io.ReadCloser) (err error) {
	defer func() {
		derr := body.Close()
		if derr != nil {
			if err != nil {
				err = errors.Wrapf(err, "error in defer")
				err = errors.Wrapf(err, derr.Error())
			} else {
				err = errors.Wrap(derr, "error in defer")
			}
		}
	}()

	var data []byte
	data, err = ioutil.ReadAll(body)
	if err != nil {
		return errors.Wrap(err, "read failed")
	}

	// Create a struct to allow parsing of the response object.
	parsed := struct {
		URL                     string `json:"Url"`
		ConnectionToken         string
		ConnectionID            string `json:"ConnectionId"`
		KeepAliveTimeout        float64
		DisconnectTimeout       float64
		ConnectionTimeout       float64
		TryWebSockets           bool
		ProtocolVersion         string
		TransportConnectTimeout float64
		LongPollDelay           float64
	}{}
	err = json.Unmarshal(data, &parsed)
	if err != nil {
		return errors.Wrap(err, "json unmarshal failed")
	}

	// Set the connection token and ID.
	c.ConnectionToken = parsed.ConnectionToken
	c.ConnectionID = parsed.ConnectionID

	// Update the protocol version.
	c.Protocol = parsed.ProtocolVersion

	// Set the SignalR endpoint.
	c.Endpoint = parsed.URL

	return nil
}

// Connect implements the connect step of the SignalR connection sequence.
func (c *Client) Connect() (*websocket.Conn, error) {
	// Example connect URL:
	// https://socket.bittrex.com/signalr/connect?
	//   transport=webSockets&
	//   clientProtocol=1.5&
	//   connectionToken=<token>&
	//   connectionData=%5B%7B%22name%22%3A%22corehub%22%7D%5D&
	//   tid=5
	// -> returns connection ID. (e.g.: d-F2577E41-B,0|If60z,0|If600,1)

	// Create the URL.
	u := makeURL("connect", c)

	// Perform the connection.
	conn, err := c.xconnect(u.String(), false)
	if err != nil {
		err = errors.Wrap(err, "xconnect failed")
		return nil, err
	}

	return conn, nil
}

// Reconnect implements the reconnect step of the SignalR connection sequence.
func (c *Client) Reconnect() (*websocket.Conn, error) {
	// Note from
	// https://blog.3d-logic.com/2015/03/29/signalr-on-the-wire-an-informal-description-of-the-signalr-protocol/
	// Once the channel is set up there are no further HTTP requests until
	// the client is stopped (the abort request) or the connection was lost
	// and the client tries to re-establish the connection (the reconnect
	// request).

	// Example reconnect URL:
	// https://socket.bittrex.com/signalr/reconnect?
	//   transport=webSockets&
	//   messageId=d-F2577E41-B%2C0%7CIf60z%2C0%7CIf600%2C1&
	//   clientProtocol=1.5&
	//   connectionToken=<same-token-as-above>&
	//   connectionData=%5B%7B%22name%22%3A%22corehub%22%7D%5D&
	//   tid=7
	// Note: messageId matches connection ID returned from the connect request

	// Create the URL.
	u := makeURL("reconnect", c)

	// Perform the reconnection.
	conn, err := c.xconnect(u.String(), true)
	if err != nil {
		return nil, errors.Wrap(err, "xconnect failed")
	}

	// Once complete, set the new connection for this client.
	c.SetConn(conn)

	return conn, nil
}

func (c *Client) xconnect(url string, isReconnect bool) (*websocket.Conn, error) {
	// Prepare to use the existing HTTP client's cookie jar, if an HTTP client
	// has been set.
	var jar http.CookieJar
	if c.HTTPClient != nil {
		jar = c.HTTPClient.Jar
	}

	// Set the proxy to use the value from the environment by default.
	proxy := http.ProxyFromEnvironment

	// Check to see if the HTTP client transport is defined.
	if t, ok := c.HTTPClient.Transport.(*http.Transport); ok {
		// If the client is an HTTP client, then it will have a proxy defined.
		// By default, this is set to http.ProxyFromEnvironment. If it is not
		// that function, then it will be some other valid function; otherwise
		// the code won't compile. Therefore, we choose to use that function as
		// our proxy.
		//
		// For details about the default value of the proxy, see here:
		// https://github.com/golang/go/blob/cf4691650c3c556f19844a881a32792a919ee8d1/src/net/http/transport.go#L43
		proxy = t.Proxy
	}

	// Create a dialer that uses the supplied TLS client configuration.
	dialer := &websocket.Dialer{
		Proxy:           proxy,
		TLSClientConfig: c.TLSClientConfig,
		Jar:             jar,
	}

	// Prepare a header to be used when dialing to the service.
	header := makeHeader(c)

	var retryCount int
	if isReconnect {
		retryCount = c.MaxReconnectRetries
	} else {
		retryCount = c.MaxConnectRetries
	}

	// Perform the connection in a retry loop.
	var conn *websocket.Conn
	var err error
	for i := 0; i < retryCount; i++ {
		var resp *http.Response
		conn, resp, err = dialer.Dial(url, header)
		if err == nil {
			// If there was no error, break out of the retry loop.
			break
		}

		// Verify that a response accompanies the error.
		if resp == nil {
			err = errors.Wrapf(err, "empty response, retry %d", i)

			// If no response is set, then wait and retry.
			time.Sleep(c.RetryWaitDuration)
			continue
		}

		// According to documentation at
		// https://godoc.org/github.com/gorilla/websocket#Dialer.Dial
		// ErrBadHandshake is the only error returned. Details reside in
		// the response, so that's how we process this error.
		err = errors.Wrapf(err, "%v, retry %d", resp.Status, i)

		// Handle any specific errors.
		switch resp.StatusCode {
		case 503:
			// Wait and retry.
			time.Sleep(c.RetryWaitDuration)
			continue
		default:
			// Return in the event that no specific error was
			// encountered.
			return nil, err
		}
	}

	return conn, err
}

// Start implements the start step of the SignalR connection sequence.
func (c *Client) Start(conn WebsocketConn) error {
	if conn == nil {
		return errors.New("connection is nil")
	}

	u := makeURL("start", c)

	req, err := prepareRequest(u.String(), c.Headers)
	if err != nil {
		return errors.Wrap(err, "request preparation failed")
	}

	// Perform the request in a retry loop.
	var resp *http.Response
	for i := 0; i < c.MaxStartRetries; i++ {
		resp, err = c.HTTPClient.Do(req)

		// Exit the retry loop if the request was successful.
		if err == nil {
			break
		}

		// If the request was unsuccessful, wrap the error, sleep, and
		// then retry.
		err = errors.Wrapf(err, "request failed (%d)", i)

		// Wait and retry.
		time.Sleep(c.RetryWaitDuration)
	}

	// If an error occurred on the last retry, then return.
	if err != nil {
		return errors.Wrap(err, "all request retries failed")
	}

	if resp == nil {
		return errors.New("response is nil")
	}

	return c.processStartResponse(resp.Body, conn)
}

func (c *Client) processStartResponse(body io.ReadCloser, conn WebsocketConn) (err error) {
	defer func() {
		derr := body.Close()
		if derr != nil {
			if err != nil {
				err = errors.Wrapf(err, "error in defer")
				err = errors.Wrapf(err, derr.Error())
			} else {
				err = errors.Wrap(derr, "error in defer")
			}
		}
	}()

	var data []byte
	data, err = ioutil.ReadAll(body)
	if err != nil {
		return errors.Wrap(err, "read failed")
	}

	// Create an anonymous struct to parse the response.
	parsed := struct{ Response string }{}
	err = json.Unmarshal(data, &parsed)
	if err != nil {
		return errors.Wrap(err, "json unmarshal failed")
	}

	// Confirm the server response is what we expect.
	if parsed.Response != "started" {
		return errors.Errorf("start response is not 'started': %s", parsed.Response)
	}

	// Wait for the init message.
	var t int
	var p []byte
	t, p, err = conn.ReadMessage()
	if err != nil {
		return errors.Wrap(err, "message read failed")
	}

	// Verify the correct response type was received.
	if t != websocket.TextMessage {
		return errors.Errorf("unexpected websocket control type: %d", t)
	}

	// Extract the server message.
	var msg Message
	err = json.Unmarshal(p, &msg)
	if err != nil {
		return errors.Wrap(err, "json unmarshal failed")
	}

	serverInitialized := 1
	if msg.S != serverInitialized {
		return errors.Errorf("unexpected S value received from server: %d | message: %s", msg.S, string(p))
	}

	if msg.G != "" {
		c.GroupsToken.Set(msg.G)
	}

	if msg.C != "" {
		c.MessageID.Set(msg.C)
	}

	// Since we got to this point, the connection is successful. So we set
	// the connection for the client.
	c.conn = conn
	return nil
}

// Run connects to the host and performs the websocket initialization routines
// that are part of the SignalR specification.
func (c *Client) Run(msgHandler MsgHandler, errHandler ErrHandler) error {
	var err error

	// Make a channel that is used to indicate that the connection
	// initialization functions have completed or errored out.
	done := make(chan bool)

	go func() {
		defer func() {
			// Once this goroutine returns, indicate that it has
			// finished executing.
			done <- true
		}()

		err = c.Negotiate()
		if err != nil {
			err = errors.Wrap(err, "negotiate failed")
			return
		}

		var conn *websocket.Conn
		conn, err = c.Connect()
		if err != nil {
			err = errors.Wrap(err, "connect failed")
			return
		}

		err = c.Start(conn)
		if err != nil {
			err = errors.Wrap(err, "start failed")
			return
		}

		// Start the read message loop.
		go c.ReadMessages(msgHandler, errHandler)
	}()

	// Wait for initialization goroutine to complete.
	<-done

	return err
}

// ReadMessages processes WebSocket messages from the underlying websocket
// connection.
func (c *Client) ReadMessages(msgHandler MsgHandler, errHandler ErrHandler) {
	for {
		if !c.readMessage(msgHandler, errHandler) {
			return
		}
	}
}

func (c *Client) readMessage(msgHandler MsgHandler, errHandler ErrHandler) bool {
	// Set the ok flag to true to indicate that more messages can/should be
	// read. Set the flag to false later on if this is no longer the case.
	ok := true

	// Prepare channels for the select statement later.
	pCh := make(chan []byte)
	errs := make(chan error)

	// Wait for a message.
	go func() {
		c.connMux.Lock()
		_, p, err := c.conn.ReadMessage()
		c.connMux.Unlock()
		if err != nil {
			errs <- err
		} else {
			pCh <- p
		}
	}()

	select {
	case p := <-pCh:
		c.processReadMessagesMessage(p, msgHandler, errHandler)
	case err := <-errs:
		errHandled := make(chan bool)
		go func() {
			v := c.processReadMessagesError(err, errHandler)
			errHandled <- v
		}()
		select {
		case ok = <-errHandled:
		case <-c.close:
			ok = false
		}
	case <-c.close:
		ok = false
	}

	return ok
}

func (c *Client) processReadMessagesMessage(p []byte, msgHandler MsgHandler, errHandler ErrHandler) {
	// Ignore KeepAlive messages.
	if len(p) == 2 && p[0] == '{' && p[1] == '}' {
		return
	}

	var msg Message
	err := json.Unmarshal(p, &msg)
	if err != nil {
		go errHandler(errors.Wrap(err, "json unmarshal failed"))
		return
	}

	// Update the groups token.
	if msg.G != "" {
		c.GroupsToken.Set(msg.G)
	}

	// Update the current message ID.
	if msg.C != "" {
		c.MessageID.Set(msg.C)
	}

	go msgHandler(msg)
}

func (c *Client) processReadMessagesError(err error, errHandler ErrHandler) bool {
	var ok bool

	// Handle various types of errors.
	// https://tools.ietf.org/html/rfc6455#section-7.4.1
	code := websocketErrCode(err)
	switch code {
	case 1000:
		// normal closure
		fallthrough
	case 1001:
		// going away
		fallthrough
	case 1006:
		// abnormal closure
		okCh := make(chan bool)
		go func() {
			v := c.attemptReconnect()
			okCh <- v
		}()
		select {
		case ok = <-okCh:
		case <-time.After(c.MaxReconnectAttemptDuration):
			// Fail if the retry exceeds the maximum amount of time for
			// reconnects to last.
			ok = false
		}
	default:
		go errHandler(err)
	}

	return ok
}

func (c *Client) attemptReconnect() bool {
	// Attempt to reconnect in a retry loop.
	reconnected := false
	for i := 0; i < c.MaxReconnectRetries; i++ {
		debugMessage("%sattempting to reconnect...", prefixedID(c.CustomID))

		_, err := c.Reconnect()
		if err != nil {
			// Ignore the value of the error and just continue.
			continue
		}

		debugMessage("%sreconnected successfully", prefixedID(c.CustomID))
		reconnected = true
		break
	}

	// If the reconnect attempt succeeded, ignore the error. Since we are
	// still within the readmessage loop, the next call to the WebSocket's
	// ReadMessage() function will use the new c.conn connection, so we
	// don't have to do any more connection repair.
	return reconnected
}

// Send sends a message to the websocket connection.
func (c *Client) Send(m hubs.ClientMsg) error {
	c.connMux.Lock()
	defer c.connMux.Unlock()

	// Verify a connection has been created.
	if c.conn == nil {
		return errors.New("send: connection not set")
	}

	// Write the message.
	err := c.conn.WriteJSON(m)
	if err != nil {
		return errors.Wrap(err, "json write failed")
	}

	return nil
}

// SetConn changes the underlying websocket connection to the specified
// connection. This is done using a mutex to wait until existing read operations
// have completed.
func (c *Client) SetConn(conn WebsocketConn) {
	c.connMux.Lock()
	defer c.connMux.Unlock()
	c.conn = conn
}

// Conn returns the underlying websocket connection.
func (c *Client) Conn() WebsocketConn {
	c.connMux.Lock()
	defer c.connMux.Unlock()
	return c.conn
}

// Close sends a signal to the loop reading WebSocket messages to indicate that
// the loop should terminate.
func (c *Client) Close() {
	c.close <- struct{}{}
}

// New creates and initializes a SignalR client.
func New(host, protocol, endpoint, connectionData string, params map[string]string) *Client {
	// Create an HTTP client that supports CloudFlare-protected sites by
	// default.
	cfTransport := scraper.NewTransport(http.DefaultTransport)
	httpClient := &http.Client{
		Transport: cfTransport,
		Jar:       cfTransport.Cookies,
	}

	return &Client{
		Host:                        host,
		Protocol:                    protocol,
		Endpoint:                    endpoint,
		ConnectionData:              connectionData,
		close:                       make(chan struct{}),
		HTTPClient:                  httpClient,
		Headers:                     make(map[string]string),
		Params:                      params,
		Scheme:                      HTTPS,
		MaxNegotiateRetries:         5,
		MaxConnectRetries:           5,
		MaxReconnectRetries:         5,
		MaxStartRetries:             5,
		RetryWaitDuration:           1 * time.Minute,
		MaxReconnectAttemptDuration: 5 * time.Minute,
	}
}

// Scheme represents a type of transport scheme. For the purposes of this
// project, we only provide constants for schemes relevant to HTTP and
// websockets.
type Scheme string

const (
	// HTTPS is the literal string, "https".
	HTTPS Scheme = "https"

	// HTTP is the literal string, "http".
	HTTP Scheme = "http"

	// WSS is the literal string, "wss".
	WSS Scheme = "wss"

	// WS is the literal string, "ws".
	WS Scheme = "ws"
)

// SafeString is a thread-safe string.
type SafeString struct {
	data string
	mux  sync.Mutex
}

// Set sets the string value.
func (s *SafeString) Set(str string) {
	s.mux.Lock()
	s.data = str
	s.mux.Unlock()
}

// Get returns the string value.
func (s *SafeString) Get() string {
	s.mux.Lock()
	defer s.mux.Unlock()
	return s.data
}

// WebsocketConn is a combination of MessageReader and JSONWriter. It is used to
// provide an interface to objects that can read from and write to a websocket
// connection.
type WebsocketConn interface {
	// ReadMessage is modeled after the function defined at
	// https://godoc.org/github.com/gorilla/websocket#Conn.ReadMessage
	//
	// At a high level, it reads messages and returns:
	//  - the type of message read
	//  - the bytes that were read
	//  - any errors encountered during reading the message
	ReadMessage() (messageType int, p []byte, err error)

	// WriteJSON is modeled after the function defined at
	// https://godoc.org/github.com/gorilla/websocket#Conn.WriteJSON
	//
	// At a high level, it writes a structure to the underlying websocket and
	// returns any error that was encountered during the write operation.
	WriteJSON(v interface{}) error
}

// Message represents a message sent from the server to the persistent websocket
// connection.
type Message struct {
	// message id, present for all non-KeepAlive messages
	C string

	// an array containing actual data
	M []hubs.ClientMsg

	// indicates that the transport was initialized (a.k.a. init message)
	S int

	// groups token – an encrypted string representing group membership
	G string

	// other miscellaneous variables that sometimes are sent by the server
	I string
	E string
	R json.RawMessage
	H json.RawMessage // could be bool or string depending on a message type
	D json.RawMessage
	T json.RawMessage
}

// MsgHandler processes a Message.
type MsgHandler func(msg Message)

// ErrHandler processes an error.
type ErrHandler func(err error)

// Conditionally encrypt the traffic depending on the initial
// connection's encryption.
func setURLScheme(u *url.URL, httpScheme Scheme) {
	if httpScheme == HTTPS {
		u.Scheme = string(WSS)
	} else {
		u.Scheme = string(WS)
	}
}

func makeURL(command string, c *Client) url.URL {
	var u url.URL

	// Set the host.
	u.Host = c.Host

	// Set the first part of the path.
	u.Path = c.Endpoint

	// Create parameters.
	params := url.Values{}

	// Add shared parameters.
	params.Set("connectionData", c.ConnectionData)
	params.Set("clientProtocol", c.Protocol)

	// Add custom user-supplied parameters.
	for k, v := range c.Params {
		params.Set(k, v)
	}

	// Set the connectionToken.
	if c.ConnectionToken != "" {
		params.Set("connectionToken", c.ConnectionToken)
	}

	connectAdjustments := func() {
		setURLScheme(&u, c.Scheme)
		params.Set("transport", "webSockets")
		params.Set("tid", fmt.Sprintf("%.0f", math.Floor(rand.Float64()*11)))
	}

	switch command {
	case "negotiate":
		u.Scheme = string(c.Scheme)
		u.Path += "/negotiate"
	case "connect":
		connectAdjustments()
		u.Path += "/connect"
	case "reconnect":
		connectAdjustments()
		if c.GroupsToken.Get() != "" {
			params.Set("groupsToken", c.GroupsToken.Get())
		}
		if c.MessageID.Get() != "" {
			params.Set("messageId", c.MessageID.Get())
		}
		u.Path += "/reconnect"
	case "start":
		u.Scheme = string(c.Scheme)
		params.Set("transport", "webSockets")
		u.Path += "/start"
	}

	// Set the parameters.
	u.RawQuery = params.Encode()

	return u
}

func prepareRequest(url string, headers map[string]string) (*http.Request, error) {
	// Make the GET request object.
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		err = errors.Wrap(err, "get request creation failed")
		return nil, err
	}

	// Add all header values.
	for k, v := range headers {
		req.Header.Add(k, v)
	}

	return req, nil
}

func makeHeader(c *Client) http.Header {
	// Create a header object that contains any cookies that have been set
	// in prior requests.
	header := make(http.Header)

	// If no client is specified, return an empty header.
	if c == nil {
		return http.Header{}
	}

	// Add cookies if they are set.
	if c.HTTPClient != nil && c.HTTPClient.Jar != nil {
		// Make a negotiate URL so we can look up the cookie that was
		// set on the negotiate request.
		nu := makeURL("negotiate", c)
		cookies := ""
		for _, v := range c.HTTPClient.Jar.Cookies(&nu) {
			if cookies == "" {
				cookies += v.Name + "=" + v.Value
			} else {
				cookies += "; " + v.Name + "=" + v.Value
			}
		}

		if cookies != "" {
			header.Add("Cookie", cookies)
		}
	}

	// Add all the other header values specified by the user.
	for k, v := range c.Headers {
		header.Add(k, v)
	}

	return header
}

func websocketErrCode(err error) int {
	re := regexp.MustCompile("[0-9]+")
	s := re.FindString(err.Error())

	var e error
	code, e := strconv.Atoi(s)
	if e != nil {
		// -1 is not a valid error code, so we use this, rather than
		// introducing the need for another error handler on the caller
		// of this function.
		code = -1
	}

	return code
}

func debugEnabled() bool {
	v := os.Getenv("DEBUG")
	return v != ""
}

func debugMessage(msg string, v ...interface{}) {
	if debugEnabled() {
		log.Printf(msg, v...)
	}
}

func prefixedID(id string) string {
	if id == "" {
		return ""
	}

	return "[" + id + "] "
}
