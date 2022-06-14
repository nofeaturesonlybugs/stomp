package server_test

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nofeaturesonlybugs/stomp"
	"github.com/nofeaturesonlybugs/stomp/frames"
	"github.com/nofeaturesonlybugs/stomp/server"
	"github.com/nofeaturesonlybugs/stomp/server/testsuite"
)

func TestPeer_Send(t *testing.T) {
	const Topic string = "/topic/test-peer"
	type PeerTest struct {
		Clients       int
		Subscriptions int
		Send          int
	}
	tests := []PeerTest{
		// Subscriptions=1
		{Clients: 1, Subscriptions: 1, Send: 1},
		{Clients: 2, Subscriptions: 1, Send: 1},
		{Clients: 25, Subscriptions: 1, Send: 1},
		{Clients: 50, Subscriptions: 1, Send: 50},
		// {Clients: 50, Subscriptions: 1, Send: 1000}, // Slows tests down a lot
		// Subscriptions=5
		{Clients: 1, Subscriptions: 5, Send: 1},
		{Clients: 2, Subscriptions: 5, Send: 1},
		{Clients: 25, Subscriptions: 5, Send: 1},
		{Clients: 50, Subscriptions: 5, Send: 50},
		// {Clients: 50, Subscriptions: 5, Send: 1000}, // Slows tests down a lot
		// Subscriptions=20
		{Clients: 1, Subscriptions: 20, Send: 1},
		{Clients: 2, Subscriptions: 20, Send: 1},
		{Clients: 25, Subscriptions: 20, Send: 1},
		{Clients: 50, Subscriptions: 20, Send: 50},
		// {Clients: 50, Subscriptions: 20, Send: 1000}, // Slows tests down a lot
	}
	Fn := func(chanBuffer int, test PeerTest, network bool, t *testing.T) {
		chk := assert.New(t)
		//
		var wg sync.WaitGroup
		//
		log := stomp.StdoutLogger
		serverMock := testsuite.MockServer{ // TODO Add log to mockserver
			ExpectPeerSend:         test.Clients * test.Subscriptions * test.Send,
			ExpectPeerClosedReader: test.Clients,
			ExpectPeerDisconnected: test.Clients,
		}
		serverMock.Start(chanBuffer, &wg)
		subscriptions := testsuite.NewSubscriptions(Topic, test.Subscriptions)
		peerFactory := testsuite.PeerFactory{
			ServerC:       serverMock.C,
			ServerSignals: serverMock.Signals,
			Logger:        log,
			WaitGroup:     &wg,
			// Test uses channel operations so call Start on locals and remotes.
			StartLocal:  true,
			StartRemote: true,
		}
		if network {
			err := peerFactory.AsNetwork()
			chk.NoError(err)
		}
		//
		for clientNum := 0; clientNum < test.Clients; clientNum++ {
			r, _, err := peerFactory.Make()
			chk.NoError(err)
			//
			// Mock remote client(s).  Each one sends test.Send messages per topic.
			wg.Add(1)
			go func(clientNum int, remote stomp.Peer) {
				defer wg.Done()
				subscriptions.Send(test.Send, r.Send)
			}(clientNum, r)
		}
		serverMock.StartPeers(peerFactory.Peers, &wg)
		<-serverMock.Signals.AwaitMessagesConsumed
		for _, remote := range peerFactory.Remotes {
			err := remote.Shutdown()
			chk.NoError(err)
		}
		//
		wg.Wait()
		chk.Empty(serverMock.Peers)
		chk.Equal(serverMock.ExpectPeerDisconnected, serverMock.ActualPeerDisconnected)
		chk.Equal(serverMock.ExpectPeerClosedReader, serverMock.ActualPeerClosedReader)
		chk.Equal(serverMock.ExpectPeerSend, serverMock.ActualPeerSend)
		if network {
			err := peerFactory.Listener.Close()
			chk.NoError(err)
		}
	}
	for _, test := range tests {
		for _, chanBuffer := range []int{0, 32, 128, 1024} {
			name := fmt.Sprintf("cb=%v clients=%v subscriptions=%v s=%v", chanBuffer, test.Clients, test.Subscriptions, test.Send)
			t.Run("pipe "+name, func(t *testing.T) {
				Fn(0, test, false, t)
			})
			t.Run("network "+name, func(t *testing.T) {
				Fn(0, test, true, t)
			})
		}
	}
}

func TestPeer_SubscribeUnsubscribe(t *testing.T) {
	const ChanBuffer int = 32
	const Topic string = "/topic/test-peer"
	//
	var wg sync.WaitGroup
	ServerC := make(chan interface{}, ChanBuffer)
	log := stomp.StdoutLogger
	//
	remote, local := stomp.Pipe()
	remote.Start(&wg)
	local.Start(&wg)
	//
	peer := server.Peer{
		Peer:          local,
		Session:       stomp.SessionID(),
		Subscriptions: map[server.PeerSubscription]struct{}{},
		ServerC:       ServerC,
		// ServerSignals: server.ServerSignals{}, // Not needed for test.
		Logger: log,
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		peer.Run()
	}()
	//
	// ExpectServer attempts to read one response from ServerC and returns an error after a timeout.
	ExpectServer := func() error {
		select {
		case opaque := <-ServerC:
			switch message := opaque.(type) {
			case server.PeerUnsubscribe: // need to mimic subscription logic and close this channel if present
				if message.Await != nil {
					close(message.Await)
				}
			}
			return nil
		case <-time.After(1 * time.Second):
			return errors.New("timeout waiting for server channel")
		}
	}
	// ExpectReceiveError attempts to read one response from remote.Receive and verifies it is an ERROR frame.
	ExpectReceiveError := func() error {
		select {
		case f := <-remote.Receive:
			if f.Command != stomp.CommandError {
				return fmt.Errorf("expect %v frame; got %v", stomp.CommandError, f.Command)
			}
		case <-time.After(1 * time.Second):
			return errors.New("timeout waiting for remote.Receive")
		}
		return nil
	}
	//
	type TestCase struct {
		Name string
		Fn   func(t *testing.T)
	}
	tests := []TestCase{
		{
			// Good SUBSCRIBE/UNSUBSCRIBE pair move forward into server channel.
			Name: "good",
			Fn: func(t *testing.T) {
				chk := assert.New(t)
				remote.Send <- frames.Subscribe(Topic, "", "")
				chk.NoError(ExpectServer())
				remote.Send <- frames.Unsubscribe(Topic, "")
				chk.NoError(ExpectServer())
			},
		},
		{
			// SUBSCRIBE request without destination header causes ERROR frame response.
			Name: "subscribe missing dest",
			Fn: func(t *testing.T) {
				chk := assert.New(t)
				//
				subscribe := stomp.Frame{
					Command: stomp.CommandSubscribe,
				}
				remote.Send <- subscribe
				chk.NoError(ExpectReceiveError())
			},
		},
		{
			// UNSUBSCRIBE request without destination header causes ERROR frame response.
			Name: "unsubscribe missing dest",
			Fn: func(t *testing.T) {
				chk := assert.New(t)
				//
				unsubscribe := stomp.Frame{
					Command: stomp.CommandUnsubscribe,
				}
				remote.Send <- unsubscribe
				chk.NoError(ExpectReceiveError())
			},
		},
		{
			// Duplicate subscription causes error on duplicate request(s).
			Name: "duplicate error",
			Fn: func(t *testing.T) {
				chk := assert.New(t)
				//
				remote.Send <- frames.Subscribe(Topic, "", "")
				chk.NoError(ExpectServer())
				remote.Send <- frames.Subscribe(Topic, "", "") // dup #1
				chk.NoError(ExpectReceiveError())
				remote.Send <- frames.Subscribe(Topic, "", "") // dup #2
				chk.NoError(ExpectReceiveError())
				remote.Send <- frames.Unsubscribe(Topic, "")
				chk.NoError(ExpectServer())
			},
		},
		{
			// Duplicate with IDs is allowed.
			Name: "duplicate with IDs",
			Fn: func(t *testing.T) {
				chk := assert.New(t)
				//
				remote.Send <- frames.Subscribe(Topic, "", "")
				chk.NoError(ExpectServer())
				for n := 0; n < 5; n++ {
					id := fmt.Sprintf("id-%v", n)
					remote.Send <- frames.Subscribe(Topic, "", id)
					chk.NoError(ExpectServer())
				}
				remote.Send <- frames.Unsubscribe(Topic, "")
				chk.NoError(ExpectServer())
				for n := 0; n < 5; n++ {
					id := fmt.Sprintf("id-%v", n)
					remote.Send <- frames.Unsubscribe(Topic, id)
					chk.NoError(ExpectServer())
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.Name, test.Fn)
	}
}

func TestPeer_DisconnectUnsubscribesAll(t *testing.T) {
	const Topic string = "/topic/test-peer-disconnect-unsubscribes-all"
	type DisconnectTest struct {
		Clients       int
		Subscriptions int
	}
	tests := []DisconnectTest{
		{Clients: 1, Subscriptions: 1},
		{Clients: 1, Subscriptions: 3},
		{Clients: 1, Subscriptions: 10},
		{Clients: 2, Subscriptions: 1},
		{Clients: 2, Subscriptions: 10},
		{Clients: 10, Subscriptions: 1},
		{Clients: 10, Subscriptions: 10},
	}
	Fn := func(chanBuffer int, test DisconnectTest, network bool, t *testing.T) {
		chk := assert.New(t)
		//
		var wg sync.WaitGroup
		log := stomp.StdoutLogger
		mockServer := testsuite.MockServer{
			ExpectPeerSend:         0,
			ExpectPeerSubscribe:    test.Clients * test.Subscriptions,
			ExpectPeerUnsubscribe:  test.Clients * test.Subscriptions,
			ExpectPeerClosedReader: test.Clients,
			ExpectPeerDisconnected: test.Clients,
		}
		mockServer.Start(chanBuffer, &wg)
		peerFactory := testsuite.PeerFactory{
			ServerC:       mockServer.C,
			ServerSignals: mockServer.Signals,
			Logger:        log,
			WaitGroup:     &wg,
			// Test uses channels so call Start on locals and remotes.
			StartLocal:  true,
			StartRemote: true,
		}
		if network {
			err := peerFactory.AsNetwork()
			chk.NoError(err)
		}
		subscriptions := testsuite.NewSubscriptions(Topic, test.Subscriptions)
		//
		for n := 0; n < test.Clients; n++ {
			remote, _, err := peerFactory.Make()
			chk.NoError(err)
			//
			wg.Add(1)
			go func(remote stomp.Peer) {
				defer wg.Done()
				for _, s := range subscriptions {
					remote.Send <- frames.Subscribe(s.Topic, "", "")
				}
				err := remote.Shutdown()
				chk.NoError(err)
			}(remote)
		}
		mockServer.StartPeers(peerFactory.Peers, &wg)
		//
		wg.Wait()
		chk.Equal(mockServer.ExpectPeerSubscribe, mockServer.ActualPeerSubscribe)
		chk.Equal(mockServer.ExpectPeerUnsubscribe, mockServer.ActualPeerUnsubscribe)
		chk.Equal(mockServer.ExpectPeerClosedReader, mockServer.ActualPeerClosedReader)
		chk.Equal(mockServer.ExpectPeerDisconnected, mockServer.ActualPeerDisconnected)
		chk.Empty(mockServer.Peers)
		if network {
			err := peerFactory.Listener.Close()
			chk.NoError(err)
		}
	}
	for _, test := range tests {
		for _, chanBuffer := range []int{0, 32, 128, 1024} {
			name := fmt.Sprintf("chan-buffer=%v clients=%v subscriptions=%v", chanBuffer, test.Clients, test.Subscriptions)
			t.Run("pipe "+name, func(t *testing.T) {
				Fn(chanBuffer, test, false, t)
			})
			t.Run("network "+name, func(t *testing.T) {
				Fn(chanBuffer, test, true, t)
			})
		}
	}
}

func TestPeer_RemotePeerEOFSendsRemaining(t *testing.T) {
	const Topic string = "/topic/test-peer"
	type SendTest struct {
		Clients       int
		Subscriptions int
		Send          int
	}
	tests := []SendTest{
		{Clients: 1, Subscriptions: 1, Send: 1},
		{Clients: 1, Subscriptions: 1, Send: 10},
		{Clients: 2, Subscriptions: 1, Send: 1},
		{Clients: 2, Subscriptions: 1, Send: 10},
		{Clients: 10, Subscriptions: 10, Send: 1},
		{Clients: 10, Subscriptions: 10, Send: 10},
	}
	Fn := func(chanBuffer int, test SendTest, network bool, t *testing.T) {
		chk := assert.New(t)
		//
		var wg sync.WaitGroup
		log := stomp.StdoutLogger
		mockServer := testsuite.MockServer{
			ExpectPeerSend:         test.Clients * test.Subscriptions * test.Send,
			ExpectPeerClosedReader: test.Clients,
			ExpectPeerDisconnected: test.Clients,
		}
		mockServer.Start(chanBuffer, &wg)
		peerFactory := testsuite.PeerFactory{
			ServerC:       mockServer.C,
			ServerSignals: mockServer.Signals,
			Logger:        log,
			WaitGroup:     &wg,
			// Test writes to remote.W directly so do not start remotes.
			// These writes may block if local is not started so start locals.
			StartLocal:  true,
			StartRemote: false,
		}
		if network {
			err := peerFactory.AsNetwork()
			chk.NoError(err)
		}
		subscriptions := testsuite.NewSubscriptions(Topic, test.Subscriptions)
		//
		for c := 0; c < test.Clients; c++ {
			// This test writes to remote.W directly so **do not** start the remote peer.
			// However we do need to start the local peer so writes on remote.W do not block.
			remote, _, err := peerFactory.Make()
			chk.NoError(err)
			//
			for n := 0; n < test.Send; n++ {
				for _, subscription := range subscriptions {
					_, err := subscription.SendFrame.WriteTo(remote.W)
					chk.NoError(err)
				}
			}
			err = remote.Close()
			chk.NoError(err)
		}
		//
		// All clients have written their data above and closed.  Now when the peers are started they
		// will receive these frames plus EOF.  Even though EOF is sent they should still deliver all queued
		// frames to ServerC.
		mockServer.StartPeers(peerFactory.Peers, &wg)
		//
		WaitC := make(chan struct{})
		go func() {
			wg.Wait()
			close(WaitC)
		}()
		select {
		case <-WaitC:
		case <-time.After(1 * time.Second):
			t.Error("timeout waiting on waitgroup")
		}
		//
		chk.Equal(mockServer.ExpectPeerSend, mockServer.ActualPeerSend)
		chk.Equal(mockServer.ExpectPeerClosedReader, mockServer.ActualPeerClosedReader)
		chk.Equal(mockServer.ExpectPeerDisconnected, mockServer.ActualPeerDisconnected)
		chk.Empty(mockServer.Peers)
		if network {
			err := peerFactory.Listener.Close()
			chk.NoError(err)
		}
	}
	for _, test := range tests {
		for _, chanBuffer := range []int{0, 32, 128, 1024} {
			name := fmt.Sprintf("chan-buffer=%v clients=%v subscriptions=%v send=%v", chanBuffer, test.Clients, test.Subscriptions, test.Send)
			t.Run("pipe "+name, func(t *testing.T) {
				Fn(chanBuffer, test, false, t)
			})
			t.Run("network "+name, func(t *testing.T) {
				Fn(chanBuffer, test, false, t)
			})
		}
	}
}

func TestPeer_ServerShutdown(t *testing.T) {
	const Topic string = "/topic/test-peer"
	type SendTest struct {
		Clients       int
		Subscriptions int
		Send          int
	}
	tests := []SendTest{
		{Clients: 1, Subscriptions: 1, Send: 1},
		{Clients: 1, Subscriptions: 1, Send: 10},
		{Clients: 2, Subscriptions: 1, Send: 1},
		{Clients: 2, Subscriptions: 1, Send: 10},
		{Clients: 10, Subscriptions: 10, Send: 1},
		{Clients: 10, Subscriptions: 10, Send: 10},
	}
	Fn := func(chanBuffer int, test SendTest, network bool, t *testing.T) {
		chk := assert.New(t)
		//
		var wg sync.WaitGroup
		log := stomp.StdoutLogger
		mockServer := testsuite.MockServer{
			ExpectPeerClosedReader: test.Clients,
			ExpectPeerDisconnected: test.Clients,
		}
		mockServer.Start(chanBuffer, &wg)
		peerFactory := testsuite.PeerFactory{
			ServerC:       mockServer.C,
			ServerSignals: mockServer.Signals,
			Logger:        log,
			WaitGroup:     &wg,
			// Test writes to remote.W directly so do not start remotes.
			// These writes may block if local is not started so start locals.
			StartLocal:  true,
			StartRemote: false,
		}
		if network {
			err := peerFactory.AsNetwork()
			chk.NoError(err)
		}
		subscriptions := testsuite.NewSubscriptions(Topic, test.Subscriptions)
		//
		for c := 0; c < test.Clients; c++ {
			// This test writes to remote.W directly so **do not** start the remote peer.
			// However we do need to start the local peer so writes on remote.W do not block.
			remote, _, err := peerFactory.Make()
			chk.NoError(err)
			//
			for n := 0; n < test.Send; n++ {
				for _, subscription := range subscriptions {
					_, err := subscription.SendFrame.WriteTo(remote.W)
					chk.NoError(err)
				}
			}
		}
		//
		// All clients have written their data.  Now start the peers but signal server shutdown
		// before any peer processing begins -- this should cause all peers (local+remote) to
		// shutdown.
		close(mockServer.Channels.SigShutdown)
		close(mockServer.Channels.SigShutdownPeers)
		mockServer.StartPeers(peerFactory.Peers, &wg)
		<-mockServer.Signals.AwaitStopped // expect server to stop
		//
		// Now start all of the remotes and verify they all receive io.EOF errors from the server shutting
		// down and that calling remote.Shutdown does not return any error.
		for _, remote := range peerFactory.Remotes {
			remote.Start(&wg)
			chk.ErrorIs(<-remote.Error, io.EOF)
			err := remote.Shutdown()
			chk.NoError(err)
		}
		//
		WaitC := make(chan struct{})
		go func() {
			wg.Wait()
			close(WaitC)
		}()
		select {
		case <-WaitC:
		case <-time.After(1 * time.Second):
			t.Error("timeout waiting on waitgroup")
		}
		//
		chk.Equal(mockServer.ExpectPeerClosedReader, mockServer.ActualPeerClosedReader)
		chk.Equal(mockServer.ExpectPeerDisconnected, mockServer.ActualPeerDisconnected)
		chk.Empty(mockServer.Peers)
		if network {
			err := peerFactory.Listener.Close()
			chk.NoError(err)
		}
	}
	for _, test := range tests {
		for _, chanBuffer := range []int{0, 32, 128, 1024} {
			name := fmt.Sprintf("chan-buffer=%v clients=%v subscriptions=%v send=%v", chanBuffer, test.Clients, test.Subscriptions, test.Send)
			t.Run("pipe "+name, func(t *testing.T) {
				Fn(chanBuffer, test, false, t)
			})
			t.Run("network "+name, func(t *testing.T) {
				Fn(chanBuffer, test, false, t)
			})
		}
	}
}
