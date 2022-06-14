package server

import (
	"sync"

	"github.com/nofeaturesonlybugs/stomp"
	"github.com/nofeaturesonlybugs/stomp/server/events"
)

// SubscriptionManager manages the server's active subscriptions.
type SubscriptionManager struct {
	// The Subscriptions by destination.
	Subscriptions map[string]Subscription

	// EventsC is an optional event channel that will emit subscription related events.
	EventsC chan<- interface{}

	// Optional logger that will be forwarded to subscriptions.
	stomp.Logger
}

// Len returns the number of existing subscriptions.
func (m SubscriptionManager) Len() int {
	return len(m.Subscriptions)
}

// Send handles a PeerSend request.
func (m SubscriptionManager) Send(req PeerSend) {
	if thread, ok := m.Subscriptions[req.Destination]; ok {
		thread.C <- req
	}
}

// Subscribe forwards the PeerSubscribe request into the necessary subscription channel.  If an existing
// subscription channel+goroutine does not already exist then a new one is started.
func (m SubscriptionManager) Subscribe(req PeerSubscribe, wg *sync.WaitGroup) {
	thread, ok := m.Subscriptions[req.Destination]
	var run bool
	if !ok {
		thread = Subscription{
			Destination: req.Destination,
			C:           make(chan interface{}, 32), //TODO Buffer size?
			Logger:      m.Logger,
		}
		// Set flag indicating this thread was newly created and needs to be run.
		// Do not place the goroutine call for thread.Run() -- it is an error
		// in the race detector.
		run = true
		//
		if m.EventsC != nil {
			m.EventsC <- events.SubscriptionStart{
				Destination: req.Destination,
			}
		}
	}
	thread.SubscriberCount++
	thread.C <- req
	m.Subscriptions[req.Destination] = thread
	//
	if run {
		wg.Add(1)
		go func() {
			defer wg.Done()
			thread.Run()
		}()
	}
}

// Unsubscribe forwards the PeerSubscribe request into the necessary subscription channel.
func (m SubscriptionManager) Unsubscribe(req PeerUnsubscribe) {
	thread, ok := m.Subscriptions[req.Destination]
	if !ok {
		return
	}
	thread.C <- req
	if thread.SubscriberCount = thread.SubscriberCount - 1; thread.SubscriberCount == 0 {
		close(thread.C)
		delete(m.Subscriptions, req.Destination)
		if m.EventsC != nil {
			m.EventsC <- events.SubscriptionStop{
				Destination: req.Destination,
			}
		}
		return
	}
	m.Subscriptions[req.Destination] = thread
}
