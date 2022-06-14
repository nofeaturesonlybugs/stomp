// Package events is a collection of events emitted by the STOMP server.
package events

type ClientConnect struct {
	SessionID string
}

type ClientDisconnect struct {
	SessionID string
}

type ServerStop struct{}

type SubscriptionStart struct {
	Destination string
}

type SubscriptionStop struct {
	Destination string
}
