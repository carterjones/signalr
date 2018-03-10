[![GoDoc](https://godoc.org/github.com/carterjones/signalr?status.svg)](https://godoc.org/github.com/carterjones/signalr)
[![CircleCI](https://circleci.com/gh/carterjones/signalr.svg?style=svg)](https://circleci.com/gh/carterjones/signalr)
[![Go Report Card](https://goreportcard.com/badge/github.com/carterjones/signalr)](https://goreportcard.com/report/github.com/carterjones/signalr)
[![Maintainability](https://api.codeclimate.com/v1/badges/c561e13d50cdd11e97a1/maintainability)](https://codeclimate.com/github/carterjones/signalr/maintainability)
[![codecov](https://codecov.io/gh/carterjones/signalr/branch/master/graph/badge.svg)](https://codecov.io/gh/carterjones/signalr)

# Overview

This is my personal attempt at implementating the client side of the WebSocket
portion of the SignalR protocol. I use it for various virtual currency trading
platforms that use SignalR.

It supports CloudFlare-protected sites by default.

## Example basic usage

```go
package main

import (
	"log"

	"github.com/carterjones/signalr"
)

func main() {
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
```

## Example complex usage

```go
package main

import (
	"log"

	"github.com/carterjones/signalr"
)

func main() {
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
```

# Documentation

- GoDoc: https://godoc.org/github.com/carterjones/signalr
- SignalR specification: https://docs.microsoft.com/en-us/aspnet/signalr/overview/
- Excellent technical deep dive of the protocol: https://blog.3d-logic.com/2015/03/29/signalr-on-the-wire-an-informal-description-of-the-signalr-protocol/

# Contribute

If anything is unclear or could be improved, please open an issue or submit a
pull request. Thanks!
