package testsuite

import (
	"sync"

	"github.com/nofeaturesonlybugs/stomp"
	"github.com/nofeaturesonlybugs/stomp/server"
)

// MockServer simulates some of the behavior for the stomp/server.Server type and is
// used to test the stomp/server.Peer type independently of the full stomp/server.Server
type MockServer struct {
	// The following values indicate how many server commands are expected and how
	// many the server receives while running.
	ActualPeerSend, ExpectPeerSend                 int
	ActualPeerSubscribe, ExpectPeerSubscribe       int
	ActualPeerUnsubscribe, ExpectPeerUnsubscribe   int
	ActualPeerClosedReader, ExpectPeerClosedReader int
	ActualPeerDisconnected, ExpectPeerDisconnected int

	// C and Signals are the mock server channel and server signal instances used by server.Peer
	// types to communicate with the server.
	C       chan interface{}
	Signals server.ServerSignals

	Peers     map[string]server.Peer // peers by session
	PeersLock sync.Mutex

	// Some of the channels in the Signals field are read-only and can not be closed by tests.
	// To allow tests to close channels as needed (to simulate the server) they can close
	// the associated channel in this struct.
	Channels struct {
		SigShutdown           chan stomp.Signal
		SigShutdownPeers      chan stomp.Signal
		AwaitMessagesConsumed chan stomp.Signal
		AwaitSubscriptions    chan stomp.Signal
		AwaitStopped          chan stomp.Signal
	}
}

// Start starts the mock server.
func (mock *MockServer) Start(chanBuffer int, wg *sync.WaitGroup) {
	C := make(chan interface{}, chanBuffer)
	SigShutdownC := make(chan stomp.Signal)
	SigShutdownPeersC := make(chan stomp.Signal)
	AwaitMessagesConsumedC := make(chan stomp.Signal)
	AwaitSubscriptionsC := make(chan stomp.Signal)
	AwaitStoppedC := make(chan stomp.Signal)

	mock.C = C
	mock.Signals = server.ServerSignals{
		SigShutdown:           SigShutdownC,
		SigShutdownPeers:      SigShutdownPeersC,
		AwaitMessagesConsumed: AwaitMessagesConsumedC,
		AwaitSubscriptions:    AwaitSubscriptionsC,
		AwaitStopped:          AwaitStoppedC,
	}
	mock.Channels = struct {
		SigShutdown           chan stomp.Signal
		SigShutdownPeers      chan stomp.Signal
		AwaitMessagesConsumed chan stomp.Signal
		AwaitSubscriptions    chan stomp.Signal
		AwaitStopped          chan stomp.Signal
	}{
		SigShutdown:           SigShutdownC,
		SigShutdownPeers:      SigShutdownPeersC,
		AwaitMessagesConsumed: AwaitMessagesConsumedC,
		AwaitSubscriptions:    AwaitSubscriptionsC,
		AwaitStopped:          AwaitStoppedC,
	}

	mock.Peers = map[string]server.Peer{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		var onceCloseAwaitMessagesConsumed sync.Once
		nPeersReading := mock.ExpectPeerClosedReader
		for opaque := range mock.C {
			switch message := opaque.(type) {
			case server.PeerClosedReader:
				mock.ActualPeerClosedReader++
				nPeersReading--
				if nPeersReading == 0 {
					// The once prevents a panic if we already closed it from: case server.PeerSend.
					onceCloseAwaitMessagesConsumed.Do(func() {
						close(mock.Channels.AwaitMessagesConsumed)
					})
				}
			case server.PeerDisconnected:
				mock.ActualPeerDisconnected++
				if mock.ExpectPeerDisconnected != 0 && mock.ActualPeerDisconnected >= mock.ExpectPeerDisconnected {
					// By using >= in our check above we will panic if we somehow get more PeerDisconnected than expected.
					close(mock.C)
					close(mock.Channels.AwaitStopped)
					return
				}
			case server.PeerSend:
				mock.ActualPeerSend++
				if mock.ExpectPeerSend != 0 && mock.ActualPeerSend >= mock.ExpectPeerSend {
					// By using >= in our check above we will panic if we somehow get more PeerSend than expected.
					close(mock.Channels.AwaitMessagesConsumed)
					// NB Call the once with an empty func so that we don't close the channel a
					//    second time for: case server.PeerClosedReader -- which would panic.
					onceCloseAwaitMessagesConsumed.Do(func() {})
				}
			case server.PeerSubscribe:
				mock.ActualPeerSubscribe++
			case server.PeerUnsubscribe:
				mock.ActualPeerUnsubscribe++
				if message.Await != nil {
					close(message.Await)
				}
			}
		}
	}()
}

// StartPeers starts a slice of peers.
func (mock *MockServer) StartPeers(peers []server.Peer, wg *sync.WaitGroup) {
	mock.PeersLock.Lock()
	defer mock.PeersLock.Unlock()
	wg.Add(len(peers))
	for _, peer := range peers {
		mock.Peers[peer.Session] = peer
		go func(peer server.Peer) {
			defer wg.Done()
			peer.Run()
			mock.PeersLock.Lock()
			delete(mock.Peers, peer.Session)
			mock.PeersLock.Unlock()
		}(peer)
	}
}
