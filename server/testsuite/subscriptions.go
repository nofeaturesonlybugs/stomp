package testsuite

import (
	"fmt"

	"github.com/nofeaturesonlybugs/stomp"
	"github.com/nofeaturesonlybugs/stomp/frames"
	"github.com/nofeaturesonlybugs/stomp/server"
)

// Subscriptions is a list of subscription types.
//
// Many of the server tests use a variable number of topic destinations and send
// variable number of messages per destination.
//
// The Subscriptions type reduces the boiler plate of generating frames, iterating topics to send
// various server commands, etc.
type Subscriptions []Subscription

// NewSubscriptions creates a subscriptions type with n subscriptions where each subscription
// has dest as a prefix and "/n" as a suffix:
func NewSubscriptions(dest string, n int) Subscriptions {
	var subs Subscriptions
	for k := 0; k < n; k++ {
		topic := fmt.Sprintf("%v/%v", dest, k)
		s := Subscription{
			Topic:     topic,
			SendFrame: frames.SendString(topic, "-"),
		}
		subs = append(subs, s)
	}
	return subs
}

// NewCountMap returns a new map[string]int where the key is the subscription destination
// and the value is initially zero.  This is useful for tests that want to count messages-by-destination.
func (subs Subscriptions) NewCountMap() map[string]int {
	var rv map[string]int = map[string]int{}
	for _, s := range subs {
		rv[s.Topic] = 0
	}
	return rv
}

// PeerSendToServer sends n PeerSend messages per Subscription to ServerC.
func (subs Subscriptions) PeerSendToServer(n int, ServerC chan<- interface{}) {
	var sends []server.PeerSend
	for _, s := range subs {
		send := server.PeerSend{
			Destination: s.Topic,
			Frame:       s.SendFrame,
		}
		sends = append(sends, send)
	}
	for k := 0; k < n; k++ {
		for _, send := range sends {
			ServerC <- send
		}
	}
}

// PeerSubscribeToServer sends a PeerSubscribe message per Subscription to ServerC for SubscriberC.
func (subs Subscriptions) PeerSubscribeToServer(ServerC chan<- interface{}, SubscriberC chan<- stomp.Frame) {
	for _, s := range subs {
		ServerC <- s.PeerSubscribe(SubscriberC, "")
	}
}

// PeerUnsubscribeToServer sends a PeerUnsubscribe message per Subscription to ServerC for SubscriberC.
func (subs Subscriptions) PeerUnsubscribeToServer(ServerC chan<- interface{}, SubscriberC chan<- stomp.Frame) {
	for _, s := range subs {
		ServerC <- s.PeerUnsubscribe(SubscriberC, "", nil)
	}
}

// Send will send each subscription's send frame to SendC n times.
func (subs Subscriptions) Send(n int, SendC chan<- stomp.Frame) {
	for k := 0; k < n; k++ {
		for _, s := range subs {
			SendC <- s.SendFrame
		}
	}
}

// Subscription contains meta information describing a destination and frame-to-send.
type Subscription struct {
	Topic     string
	SendFrame stomp.Frame
}

// PeerSubscribe returns a PeerSubscribe type for this subscription.
func (s Subscription) PeerSubscribe(c chan<- stomp.Frame, id string) server.PeerSubscribe {
	return server.PeerSubscribe{
		Destination: s.Topic,
		SubscriptionPeer: server.SubscriptionPeer{
			ID:   id,
			Send: c,
		},
	}
}

// PeerUnsubscribe returns a PeerUnsubscribe type for this subscription.
func (s Subscription) PeerUnsubscribe(c chan<- stomp.Frame, id string, await chan<- stomp.Signal) server.PeerUnsubscribe {
	return server.PeerUnsubscribe{
		Destination: s.Topic,
		SubscriptionPeer: server.SubscriptionPeer{
			ID:   id,
			Send: c,
		},
		Await: await,
	}
}
