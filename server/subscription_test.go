package server_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nofeaturesonlybugs/stomp"
	"github.com/nofeaturesonlybugs/stomp/server"
	"github.com/nofeaturesonlybugs/stomp/server/testsuite"
)

func TestSubscription(t *testing.T) {
	const Topic string = "/topic/test-subscription"
	type SubscriptionTest struct {
		ChanBuffer    int
		Clients       int
		Subscriptions int
		Send          int
	}
	tests := []SubscriptionTest{
		// Subscriptions=1
		{ChanBuffer: 0, Clients: 1, Subscriptions: 1, Send: 1},
		{ChanBuffer: 0, Clients: 2, Subscriptions: 1, Send: 1},
		{ChanBuffer: 0, Clients: 25, Subscriptions: 1, Send: 1},
		{ChanBuffer: 0, Clients: 50, Subscriptions: 1, Send: 50},
		{ChanBuffer: 0, Clients: 50, Subscriptions: 1, Send: 1000},
		// Subscriptions=5
		{ChanBuffer: 0, Clients: 1, Subscriptions: 5, Send: 1},
		{ChanBuffer: 0, Clients: 2, Subscriptions: 5, Send: 1},
		{ChanBuffer: 0, Clients: 25, Subscriptions: 5, Send: 1},
		{ChanBuffer: 0, Clients: 50, Subscriptions: 5, Send: 50},
		{ChanBuffer: 0, Clients: 50, Subscriptions: 5, Send: 1000},
		// Subscriptions=20
		{ChanBuffer: 0, Clients: 1, Subscriptions: 20, Send: 1},
		{ChanBuffer: 0, Clients: 2, Subscriptions: 20, Send: 1},
		{ChanBuffer: 0, Clients: 25, Subscriptions: 20, Send: 1},
		{ChanBuffer: 0, Clients: 50, Subscriptions: 20, Send: 50},
		{ChanBuffer: 0, Clients: 50, Subscriptions: 20, Send: 1000},
	}
	for _, test := range tests {
		name := fmt.Sprintf("chan-buffer=%v clients=%v subscriptions=%v send=%v", test.ChanBuffer, test.Clients, test.Subscriptions, test.Send)
		t.Run(name, func(t *testing.T) {
			var wg sync.WaitGroup
			//
			subscriptions := testsuite.NewSubscriptions(Topic, test.Subscriptions)
			//
			ServerC := make(chan interface{}, test.ChanBuffer)
			wg.Add(1)
			go func() { // mock Server routine
				defer wg.Done()
				submgr := server.SubscriptionManager{
					Subscriptions: map[string]server.Subscription{},
					Logger:        stomp.StdoutLogger,
				}
			MockServerLoop:
				for {
					select {
					case opaque := <-ServerC:
						switch req := opaque.(type) {
						case server.PeerSend:
							submgr.Send(req)
						case server.PeerSubscribe:
							submgr.Subscribe(req, &wg)
						case server.PeerUnsubscribe:
							submgr.Unsubscribe(req)
							if submgr.Len() == 0 {
								break MockServerLoop
							}
						}
					}
				}
				close(ServerC)
			}()
			//
			var wgSubscribers sync.WaitGroup
			wg.Add(test.Clients)
			wgSubscribers.Add(test.Clients)
			for n := 0; n < test.Clients; n++ {
				go func() {
					defer wg.Done()
					c := make(chan stomp.Frame, test.ChanBuffer)    // subscription channel
					subscriptions.PeerSubscribeToServer(ServerC, c) // subscribe to all
					wgSubscribers.Done()
					//
					messagesByDest := map[string]int{}
					got := 0
					for f := range c {
						messagesByDest[f.Headers[stomp.HeaderDestination]]++
						if got = got + 1; got == test.Subscriptions*test.Send {
							break
						}
					}
					//
					for _, s := range subscriptions {
						if n := messagesByDest[s.Topic]; n != test.Send {
							t.Errorf("%v got %v; expected %v", s.Topic, n, test.Send)
						}
					}
					subscriptions.PeerUnsubscribeToServer(ServerC, c) // unsubscribe from all
				}()
			}
			//
			wgSubscribers.Wait()                               // wait for subscribers to register their subscriptions
			subscriptions.PeerSendToServer(test.Send, ServerC) // send test.Send messages per subscription
			//
			WaitC := make(chan stomp.Signal)
			go func() {
				wg.Wait()
				close(WaitC)
			}()
			//
			select {
			case <-WaitC:
			case <-time.After(1 * time.Second):
				close(WaitC)
				t.Fatal("timeout waiting on goroutines to end cleanly")
			}
		})
	}
}

func TestSubscriptionDuplicateSubscriptionPanics(t *testing.T) {
	const Topic string = "/topic/test-subscription"
	//
	log := stomp.StdoutLogger
	s := server.Subscription{
		Destination: Topic,
		C:           make(chan interface{}, 0),
		Logger:      log,
	}
	ErrC := make(chan error)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ErrC <- nil
			} else {
				ErrC <- errors.New("duplicate subscribe should panic")
			}
		}()
		s.Run()
	}()
	send := make(chan stomp.Frame)
	subscribe := server.PeerSubscribe{
		Destination: Topic,
		SubscriptionPeer: server.SubscriptionPeer{
			ID:   "",
			Send: send,
		},
	}
	s.C <- subscribe
	s.C <- subscribe
	close(s.C)
	if err := <-ErrC; err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionUnsubscribeWithAwait(t *testing.T) {
	const Topic string = "/topic/test-subscription"
	//
	log := stomp.StdoutLogger
	s := server.Subscription{
		Destination: Topic,
		C:           make(chan interface{}, 0),
		Logger:      log,
	}
	DoneC := make(chan stomp.Signal)
	go func() {
		defer func() {
			close(DoneC)
		}()
		s.Run()
	}()
	send := make(chan stomp.Frame)
	await := make(chan stomp.Signal)
	subscribe := server.PeerSubscribe{
		Destination: Topic,
		SubscriptionPeer: server.SubscriptionPeer{
			ID:   "",
			Send: send,
		},
	}
	unsubscribe := server.PeerUnsubscribe{
		Destination: Topic,
		SubscriptionPeer: server.SubscriptionPeer{
			ID:   "",
			Send: send,
		},
		Await: await,
	}
	s.C <- subscribe
	s.C <- unsubscribe
	close(s.C)
	select {
	case <-await:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting on unsubscribe await")
	}
	<-DoneC
}

func BenchmarkSubscriptionManager(b *testing.B) {
	const Topic string = "/topic/test-subscription"
	type SubscriptionBenchmark struct {
		ChanBuffer    int
		Clients       int
		Subscriptions int
		Send          int
	}
	benches := []SubscriptionBenchmark{
		// Subscriptions=1
		{ChanBuffer: 0, Clients: 1, Subscriptions: 1, Send: 1},
		{ChanBuffer: 0, Clients: 2, Subscriptions: 1, Send: 1},
		{ChanBuffer: 0, Clients: 25, Subscriptions: 1, Send: 1},
		{ChanBuffer: 0, Clients: 50, Subscriptions: 1, Send: 50},
		{ChanBuffer: 0, Clients: 50, Subscriptions: 1, Send: 1000},
		// Subscriptions=5
		{ChanBuffer: 0, Clients: 1, Subscriptions: 5, Send: 1},
		{ChanBuffer: 0, Clients: 2, Subscriptions: 5, Send: 1},
		{ChanBuffer: 0, Clients: 25, Subscriptions: 5, Send: 1},
		{ChanBuffer: 0, Clients: 50, Subscriptions: 5, Send: 50},
		{ChanBuffer: 0, Clients: 50, Subscriptions: 5, Send: 1000},
		// Subscriptions=20
		{ChanBuffer: 0, Clients: 1, Subscriptions: 20, Send: 1},
		{ChanBuffer: 0, Clients: 2, Subscriptions: 20, Send: 1},
		{ChanBuffer: 0, Clients: 25, Subscriptions: 20, Send: 1},
		{ChanBuffer: 0, Clients: 50, Subscriptions: 20, Send: 50},
		{ChanBuffer: 0, Clients: 50, Subscriptions: 20, Send: 1000},
	}
	for _, bench := range benches {
		name := fmt.Sprintf("chan-buffer=%v c=%v subscriptions=%v s=%v", bench.ChanBuffer, bench.Clients, bench.Subscriptions, bench.Send)
		b.Run(name, func(b *testing.B) {
			subscriptions := testsuite.NewSubscriptions(Topic, bench.Subscriptions)
			for n := 0; n < b.N; n++ {
				var wg sync.WaitGroup
				ServerC := make(chan interface{}, bench.ChanBuffer)
				wg.Add(1)
				go func() { // mock Server routine
					defer wg.Done()
					subscriptions := server.SubscriptionManager{
						Subscriptions: map[string]server.Subscription{},
						Logger:        stomp.NilLogger,
					}
				MockServerLoop:
					for {
						select {
						case opaque := <-ServerC:
							switch req := opaque.(type) {
							case server.PeerSend:
								subscriptions.Send(req)
							case server.PeerSubscribe:
								subscriptions.Subscribe(req, &wg)
							case server.PeerUnsubscribe:
								subscriptions.Unsubscribe(req)
								if subscriptions.Len() == 0 {
									break MockServerLoop
								}
							}
						}
					}
					close(ServerC)
				}()
				//
				var wgSubscribers sync.WaitGroup
				wg.Add(bench.Clients)
				wgSubscribers.Add(bench.Clients)
				for n := 0; n < bench.Clients; n++ {
					go func() {
						defer wg.Done()
						c := make(chan stomp.Frame, bench.ChanBuffer)   // subscription channel
						subscriptions.PeerSubscribeToServer(ServerC, c) // subscribe to all
						wgSubscribers.Done()
						//
						byTopic := map[string]int{}
						total := 0
						for f := range c {
							byTopic[f.Headers[stomp.HeaderDestination]]++
							if total = total + 1; total == bench.Subscriptions*bench.Send {
								break
							}
						}
						//
						for _, s := range subscriptions {
							if n := byTopic[s.Topic]; n != bench.Send {
								b.Errorf("%v got %v; expected %v", s.Topic, n, bench.Send)
							}
						}
						subscriptions.PeerUnsubscribeToServer(ServerC, c) // unsubscribe from all
					}()
				}
				//
				wgSubscribers.Wait()                                // wait for subscribers to register subscriptions
				subscriptions.PeerSendToServer(bench.Send, ServerC) // send bench.Send messages per subscription
				WaitC := make(chan struct{})
				go func() {
					wg.Wait()
					close(WaitC)
				}()
				select {
				case <-WaitC:
				case <-time.After(1 * time.Second):
					close(WaitC)
					b.Fatal("timeout waiting on goroutines to end cleanly")
				}
			}
		})
	}
}
