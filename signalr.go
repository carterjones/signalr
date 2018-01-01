// Package signalr provides the client side implementation of the WebSocket
// portion of the SignalR protocol. This was almost entirely written using
// https://blog.3d-logic.com/2015/03/29/signalr-on-the-wire-an-informal-description-of-the-signalr-protocol/
// as a reference guide.
package signalr

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/carterjones/helpers/trace"
	"github.com/carterjones/signalr/hubs"
	"github.com/gorilla/websocket"
)

// MessageReader is the interface that wraps ReadMessage.
//
// ReadMessage is defined at
// https://godoc.org/github.com/gorilla/websocket#Conn.ReadMessage
//
// At a high level, it reads messages and returns:
//  - the type of message read
//  - the bytes that were read
//  - any errors encountered during reading the message
type MessageReader interface {
	ReadMessage() (messageType int, p []byte, err error)
}

// JSONWriter is the interface that wraps WriteJSON.
//
// WriteJSON is defined at
// https://godoc.org/github.com/gorilla/websocket#Conn.WriteJSON
//
// At a high level, it writes a structure to the underlying websocket and
// returns any error that was encountered during the write operation.
type JSONWriter interface {
	WriteJSON(v interface{}) error
}

// WebsocketConn is a combination of MessageReader and JSONWriter. It is used to
// provide an interface to objects that can read from and write to a websocket
// connection.
type WebsocketConn interface {
	MessageReader
	JSONWriter
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

// Client represents a SignlR client. It manages connections so that the caller
// doesn't have to.
type Client struct {
	// The host providing the SignalR service.
	Host string

	// The relative path where the SignalR service is provided.
	Endpoint string

	// The websockets protocol version.
	Protocol string

	ConnectionData string

	// The HTTPClient used to initialize the websocket connection.
	HTTPClient *http.Client

	// This field holds a struct that can read messages from and write JSON
	// objects to a websocket. In practice, this is simply a raw websocket
	// connection that results from a successful connection to the SignalR
	// server.
	Conn WebsocketConn

	// An optional setting to provide a non-default TLS configuration to use
	// when connecting to the websocket.
	TLSClientConfig *tls.Config

	// Either HTTPS or HTTP.
	Scheme Scheme

	// Set a maximum number of negotiate retries.
	MaxNegotiateRetries int

	// The time to wait before retrying, in the event that an error occurs
	// when contacting the SignalR service.
	RetryWaitDuration time.Duration

	messages        chan Message
	ConnectionToken string
	ConnectionID    string

	// Header values that should be applied to all HTTP requests.
	Headers map[string]string
}

// Conditionally encrypt the traffic depending on the initial
// connection's encryption.
func (c *Client) setWebsocketURLScheme(u *url.URL) {
	if c.Scheme == HTTPS {
		u.Scheme = string(WSS)
	} else {
		u.Scheme = string(WS)
	}
}

func (c *Client) makeURL(command string) (u url.URL) {
	// Set the host.
	u.Host = c.Host

	// Set the first part of the path.
	u.Path = c.Endpoint

	// Create parameters.
	params := url.Values{}

	// Add shared parameters.
	params.Set("connectionData", c.ConnectionData)
	params.Set("clientProtocol", c.Protocol)

	// Set the connectionToken.
	if c.ConnectionToken != "" {
		params.Set("connectionToken", c.ConnectionToken)
	}

	switch command {
	case "negotiate":
		u.Scheme = string(c.Scheme)
		u.Path += "/negotiate"
	case "connect":
		c.setWebsocketURLScheme(&u)
		params.Set("transport", "webSockets")
		u.Path += "/connect"
	case "reconnect":
		c.setWebsocketURLScheme(&u)
		params.Set("transport", "webSockets")
		u.Path += "/reconnect"
	case "start":
		u.Scheme = string(c.Scheme)
		params.Set("transport", "webSockets")
		u.Path += "/start"
	}

	// Set the parameters.
	u.RawQuery = params.Encode()

	return
}

func (c *Client) prepareRequest(url string) (req *http.Request, err error) {
	req, err = http.NewRequest("GET", url, nil)
	if err != nil {
		trace.Error(err)
		return
	}

	// Add all header values.
	for k, v := range c.Headers {
		req.Header.Add(k, v)
	}

	return
}

// Negotiate implements the negotiate step of the SignalR connection sequence.
func (c *Client) Negotiate() (err error) {
	// Reset the connection token in case it has been set.
	c.ConnectionToken = ""

	// Make a "negotiate" URL.
	u := c.makeURL("negotiate")

	for i := 0; i < c.MaxNegotiateRetries; i++ {
		var req *http.Request
		req, err = c.prepareRequest(u.String())
		if err != nil {
			trace.Error(err)
			return
		}

		// Perform the request.
		var resp *http.Response
		resp, err = c.HTTPClient.Do(req)
		if err != nil {
			trace.Error(err)
			return
		}

		defer func() {
			derr := resp.Body.Close()
			if derr != nil {
				trace.Error(derr)
			}
		}()

		// Perform operations specific to the status code.
		switch resp.StatusCode {
		case 200:
			// Everything worked, so do nothing.
		case 503:
			// Trace the error, but don't return.
			err = errors.New(resp.Status)
			trace.Error(err)

			// Keep trying.
			time.Sleep(c.RetryWaitDuration)
			continue
		default:
			// Trace the error, but don't return.
			err = errors.New(resp.Status)
			trace.Error(err)

			// Keep trying.
			time.Sleep(c.RetryWaitDuration)
			continue
		}

		var body []byte
		body, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			trace.Error(err)
			return
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
		err = json.Unmarshal(body, &parsed)
		if err != nil {
			trace.Error(err)
			return
		}

		// Set the connection token and ID.
		c.ConnectionToken = parsed.ConnectionToken
		c.ConnectionID = parsed.ConnectionID

		// Update the protocol version.
		c.Protocol = parsed.ProtocolVersion

		// Set the SignalR endpoint.
		c.Endpoint = parsed.URL

		return
	}

	return
}

func (c *Client) xconnect(u url.URL) (conn *websocket.Conn, err error) {
	// Create a dialer that uses the supplied TLS client configuration.
	dialer := &websocket.Dialer{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: c.TLSClientConfig,
	}

	// Create a header object that contains any cookies that have been set
	// in prior requests.
	header := make(http.Header)
	if c.HTTPClient.Jar != nil {
		// Make a negotiate URL so we can look up the cookie that was
		// set on the negotiate request.
		nu := c.makeURL("negotiate")
		cookies := ""
		for _, v := range c.HTTPClient.Jar.Cookies(&nu) {
			if cookies == "" {
				cookies += v.Name + "=" + v.Value
			} else {
				cookies += "; " + v.Name + "=" + v.Value
			}
		}

		header.Add("Cookie", cookies)
	}

	// Add all the other header values specified by the user.
	for k, v := range c.Headers {
		header.Add(k, v)
	}

	// Perform the connection.
	conn, _, err = dialer.Dial(u.String(), header)
	if err != nil {
		trace.Error(err)
	}

	return
}

// Connect implements the connect step of the SignalR connection sequence.
func (c *Client) Connect() (conn *websocket.Conn, err error) {
	// Example connect URL:
	// https://socket.bittrex.com/signalr/connect?
	//   transport=webSockets&
	//   clientProtocol=1.5&
	//   connectionToken=<token>&
	//   connectionData=%5B%7B%22name%22%3A%22corehub%22%7D%5D&
	//   tid=5
	// -> returns connection ID. (e.g.: d-F2577E41-B,0|If60z,0|If600,1)

	// Create the URL.
	u := c.makeURL("connect")

	// Perform the connection.
	conn, err = c.xconnect(u)
	if err != nil {
		trace.Error(err)
	}

	return
}

// Start implements the start step of the SignalR connection sequence.
func (c *Client) Start(conn WebsocketConn) (err error) {
	u := c.makeURL("start")

	var req *http.Request
	req, err = c.prepareRequest(u.String())
	if err != nil {
		trace.Error(err)
		return
	}

	// Perform the request.
	var resp *http.Response
	resp, err = c.HTTPClient.Do(req)
	if err != nil {
		trace.Error(err)
		return
	}

	defer func() {
		derr := resp.Body.Close()
		if derr != nil {
			trace.Error(derr)
		}
	}()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		trace.Error(err)
		return
	}

	// Create an anonymous struct to parse the response.
	parsed := struct{ Response string }{}
	err = json.Unmarshal(body, &parsed)
	if err != nil {
		trace.Error(err)
		trace.DebugMessage("body: %s", string(body))
		return
	}

	// Confirm the server response is what we expect.
	if parsed.Response != "started" {
		err = errors.New("start response is not 'started': " + parsed.Response)
		trace.Error(err)
		return
	}

	// Wait for the init message.
	t, p, err := conn.ReadMessage()
	if err != nil {
		trace.Error(err)
		return
	}

	// Verify the correct response type was received.
	if t != websocket.TextMessage {
		err = errors.New("unexpected websocket control type:" + strconv.Itoa(t))
		trace.Error(err)
		return
	}

	// Extract the server message.
	var pcm Message
	err = json.Unmarshal(p, &pcm)
	if err != nil {
		trace.Error(err)
		return
	}

	serverInitialized := 1
	if pcm.S != serverInitialized {
		err = errors.New("unexpected S value received from server: " + strconv.Itoa(pcm.S))
		trace.Error(err)
		return
	}

	// Since we got to this point, the connection is successful. So we set
	// the connection for the client.
	c.Conn = conn
	return
}

// Reconnect implements the reconnect step of the SignalR connection sequence.
func (c *Client) Reconnect() (conn *websocket.Conn, err error) {
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
	u := c.makeURL("reconnect")

	// Perform the connection.
	conn, err = c.xconnect(u)
	if err != nil {
		trace.Error(err)
	}

	return
}

// Init connects to the host and performs the websocket initialization routines
// that are part of the SignalR specification.
func (c *Client) Init() (err error) {
	err = c.Negotiate()
	if err != nil {
		trace.Error(err)
		return
	}

	var conn *websocket.Conn
	conn, err = c.Connect()
	if err != nil {
		trace.Error(err)
		return
	}

	err = c.Start(conn)
	if err != nil {
		trace.Error(err)
		return
	}

	// Start the read message loop.
	go c.readMessages()

	return
}

func (c *Client) readMessages() {
	for {
		_, p, err := c.Conn.ReadMessage()

		if err != nil {
			trace.Error(err)

			// Handle various types of errors.
			if strings.Contains(err.Error(), "websocket: close 1006 (abnormal closure)") {
				// Attempt to reconnect until success. If at
				// first you don't succeed, try the same thing
				// over and over and over and over and over...
				for {
					_, ierr := c.Reconnect()
					if err != nil {
						trace.Error(ierr)
						continue
					}

					break
				}

				// Once successfully reconnected, start the read
				// message loop again.
				go c.readMessages()
				return
			}

			// Default behavior is to just return.
			return
		}

		// Ignore KeepAlive messages.
		if string(p) == "{}" {
			continue
		}

		var msg Message
		err = json.Unmarshal(p, &msg)
		if err != nil {
			trace.Error(err)
			return
		}

		c.messages <- msg
	}
}

// Send sends a message to the websocket connection.
func (c *Client) Send(m hubs.ClientMsg) (err error) {
	// Verify a connection has been created.
	if c.Conn == nil {
		err = errors.New("send: connection not set")
		trace.Error(err)
		return
	}

	// Write the message.
	err = c.Conn.WriteJSON(m)
	if err != nil {
		trace.Error(err)
		return
	}

	return
}

// Messages returns the channel that receives persistent connection messages.
func (c *Client) Messages() <-chan Message {
	return c.messages
}

// New creates and initializes a SignalR client.
func New(host, protocol, endpoint, connectionData string) (c *Client) {
	c = new(Client)
	c.Host = host
	c.Protocol = protocol
	c.Endpoint = endpoint
	c.ConnectionData = connectionData
	c.messages = make(chan Message)
	c.HTTPClient = new(http.Client)
	c.Headers = make(map[string]string)

	// Default to using a secure scheme.
	c.Scheme = HTTPS

	// Set the default max number of negotiate retries.
	c.MaxNegotiateRetries = 5

	// Set the default sleep duration between retries.
	c.RetryWaitDuration = 1 * time.Minute

	return
}
