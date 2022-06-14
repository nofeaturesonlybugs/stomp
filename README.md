[![Go Reference](https://pkg.go.dev/badge/github.com/nofeaturesonlybugs/stomp.svg)](https://pkg.go.dev/github.com/nofeaturesonlybugs/stomp)
[![Go Report Card](https://goreportcard.com/badge/github.com/nofeaturesonlybugs/stomp)](https://goreportcard.com/report/github.com/nofeaturesonlybugs/stomp)
[![Build Status](https://app.travis-ci.com/nofeaturesonlybugs/stomp.svg?branch=master)](https://app.travis-ci.com/nofeaturesonlybugs/stomp)
[![codecov](https://codecov.io/gh/nofeaturesonlybugs/stomp/branch/master/graph/badge.svg)](https://codecov.io/gh/nofeaturesonlybugs/stomp)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

`stomp` is a partial implementation of a STOMP client and broker.

It is unlikely this project will ever be fully completed.

## Client

There is no actual `Client` struct in this package; code exists but it is incomplete and commented out. The following sections describe how a stomp.Peer can be used as a low-level client, meaning it has no inbuilt features for acknowledgement, heartbeats, protocol negotiation, etc. Despite the lack of features the stomp.Peer is fairly well tested and seemingly reliable.

**Connecting**

```go
func Connect() (stomp.Peer, error) {
	var peer stomp.Peer
	//
	conn, err := tls.Dial("tcp", Address, TLSConf) // Address, TLSConf are just placeholders
	if err != nil {
		return peer, error
	}
	//
	peer.R, peer.W = conn, conn
	peer.Start(nil) // TODO Can pass a waitgroup here.
	//
	peer.Send <- frames.Connect("user123", "s3cr3t")
	select {
	case f := <-peer.Receive:
		if f.Command != stomp.CommandConnected {
			peer.Shutdown()
			return peer, fmt.Errorf("main: stomp-conf-connect: expected %v but got %v", stomp.CommandConnected, f.Command)
		}
		return peer, nil
	case err = <-peer.Error:
		peer.Shutdown()
		return peer, err
	}
}
```

**Subscribing**

```go
	var peer stomp.Peer
	topics := []string{
		"/topic/foo",
		"/topic/bar",
	}
	for _, t := range topics {
		peer.Send <- frames.Subscribe(t, "auto", "") // Auto for ack: auto
	}
```

**Send Messages**

```go
	f := frames.Send("/topic/foo", []byte("Hello"))
	f.Headers[stomp.HeaderReplyTo] = "/temp-queue/"
	peer.Send <- f

	f := frames.SendString("/topic/foo", "Goodbye")
	f.Headers[stomp.HeaderReplyTo] = "/temp-queue/"
	peer.Send <- f
```

**Receive Messages**

```go
	// This message loop ends on the first error or when a channel is closed.
	var peer stomp.Peer
MessageLoop:
	for {
		select {
		case f, open := <-peer.Receive:
			if !open {
				break MessageLoop
			}
			fmt.Printf("frame: %v", f)
		case err, open := <-peer.Error:
			if !open {
				break MessageLoop
			}
			fmt.Printf("error: %v", err)
		}
	}
```

Alternate message loop:

```go
	// This message loop stays open until both channels are closed.  This may
	// be ideal if the error comes through while messages are still pending
	// on the Receive channel.
	var peer stomp.Peer
	receiveC, errorC := peer.Receive, peer.Error
	for receiveC != nil || errorC != nil {
		select {
		case f, open := <-receiveC:
			if !open {
				receiveC = nil
				continue
			}
			fmt.Printf("frame: %v", f)
		case err, open := <-errorC:
			if !open {
				errorC = nil
				continue
			}
			fmt.Printf("error: %v", err)
		}
	}
```

## Server

The `server` package has a partially complete broker implementation.

**Starting a broker**

```go
	srv := server.Server{
		// Bind address.
		Addr: "0.0.0.0:61613",

		// Optional event channel.  If set then it must be consumed.
		// Events:    make(chan<- interface{}),

		// Optional TLS configuration.  If set then a TLS listener is created.
		// TLSConfig: &tls.Config{},

		// Optional logger.
		// Logger:    nil,

		// Optional session ID generator.
		// NewSessionID: func() string { ... },
	}
	err := srv.ListenAndServe() // ListenAndServe is non-blocking
	if err != nil {
		fmt.Println(err)
		return
	}
	defer srv.Shutdown()

	// TODO Wait on SigC or other quit mechanism
```

The server does not fully implement the STOMP protocol.

What works:

-   ✓ Server start and stop with ordered shutdown
-   ✓ Subscribe and unsubscribe

All destinations are treated as pubsub.

Missing STOMP features:

-   ⭴ Version negotiation
-   ⭴ Heartbeats
-   ⭴ DISCONNECT
-   ⭴ ACK, NACK
-   ⭴ BEGIN, COMMIT, ABORT
-   ⭴ RECEIPT

Some message headers are not fully implemented. For example SUBSCRIBE can include an `id` header (supported) which should be sent as `subscription` header in MESSAGE frame (not supported).

Additionally high level broker features are missing:

-   ⭴ Authentication and/or RBAC
-   ⭴ Message expiration
-   ⭴ Client reconnect and resume
-   ⭴ Round-robin destinations (i.e. QUEUES in ActiveMQ)
