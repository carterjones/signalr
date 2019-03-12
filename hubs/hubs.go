// Package hubs provides functionality used by the SignalR Hubs API. This was
// almost entirely written using
// https://blog.3d-logic.com/2015/03/29/signalr-on-the-wire-an-informal-description-of-the-signalr-protocol/
// as a reference guide.
package hubs

import (
	"encoding/json"

	"github.com/pkg/errors"
)

// ClientMsg represents a message sent to the Hubs API from the client.
type ClientMsg struct {
	// invocation identifier – allows to match up responses with requests
	I int

	// the name of the hub
	H string

	// the name of the method
	M string

	// arguments (an array, can be empty if the method does not have any
	// parameters)
	A []interface{}

	// state – a dictionary containing additional custom data (optional)
	S *json.RawMessage `json:",omitempty"`
}

// MarshalJSON converts the current message into a JSON-formatted byte array. It
// will perform different types of conversion based on the Golang type of the
// "A" field. For instance, an array will be converted into a JSON object
// looking like [...], whereas a byte array would look like "...".
func (cm *ClientMsg) MarshalJSON() (buf []byte, err error) {
	var args []byte
	for _, a := range cm.A {
		switch a := a.(type) {
		case []byte:
			args = append(args, a...)
		case string:
			args = append(args, a...)
		default:
			err = errors.New("unsupported argument type")
			return
		}
	}

	return json.Marshal(&struct {
		I int
		H string
		M string
		A []byte
		S *json.RawMessage `json:"omitempty"`
	}{
		I: cm.I,
		H: cm.H,
		M: cm.M,
		A: args,
		S: cm.S,
	})
}

// ServerMsg represents a message sent to the Hubs API from the server.
type ServerMsg struct {
	// invocation Id (always present)
	I int

	// the value returned by the server method (present if the method is not
	// void)
	R *json.RawMessage `json:",omitempty"`

	// error message
	E *string `json:",omitempty"`

	// true if this is a hub error
	H *bool `json:",omitempty"`

	// an object containing additional error data (can only be present for
	// hub errors)
	D *json.RawMessage `json:",omitempty"`

	// stack trace (if detailed error reporting (i.e. the
	// HubConfiguration.EnableDetailedErrors property) is turned on on the
	// server)
	T *json.RawMessage `json:",omitempty"`

	// state – a dictionary containing additional custom data (optional)
	S *json.RawMessage `json:",omitempty"`
}
