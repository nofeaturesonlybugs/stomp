package stomp_test

import (
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nofeaturesonlybugs/stomp"
	"github.com/nofeaturesonlybugs/stomp/frames"
)

func TestPeer_PipeShutdown(t *testing.T) {
	// Peers created with stomp.Pipe are moderately difficult to Shutdown in a way that does not have
	// errors or somehow blocks indefinitely; repeatedly while writing the stomp/server package I ran
	// into issues with peers blocking during Shutdown.
	//
	// Nearly all of these instances are tied to the Peer.reader method that:
	//	a) blocks unless data is ready or the remote pipe endpoint is closed or
	//	b) wakes with a frame but can't push it into the frame channel because the channel
	//     buffer is full and the client is no longer consuming the channel.
	//
	// Peers created with stomp.Pipe now have handles to each other's remote W pipe and can close the
	// "remote" writer to wake up their local reader.  This causes an io.ErrClosedPipe in the remote
	// Peer.writer method and typically a stomp.ErrFrame in the local Peer.reader; both of these
	// errors are expected and do not filter into the Peer.Error channel.
	type PeerTest struct {
		Peers int
		Send  int
	}
	tests := []PeerTest{
		{Peers: 1, Send: 1000},
		{Peers: 10, Send: 1000},
		{Peers: 100, Send: 1000},
		{Peers: 1000, Send: 1000},
	}
	for _, test := range tests {
		name := fmt.Sprintf("peers=%v send=%v", test.Peers, test.Send)
		t.Run(name, func(t *testing.T) {
			chk := assert.New(t)
			var wg sync.WaitGroup
			PeerErrorC := make(chan error, 2*test.Peers)
			ConsumeErrorC := make(chan error, test.Peers)
			ProduceErrorC := make(chan error, test.Peers)
			for n := 0; n < test.Peers; n++ {
				consume, produce := stomp.Pipe()
				consume.Start(&wg)
				produce.Start(&wg)
				ConsumeStartC := make(chan struct{})
				//
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-ConsumeStartC
					// When ConsumeStartC is closed our Receive channel plus IO memory pipe is full and we consume
					// a single frame, break, and call Shutdown.  The call to Shutdown should correctly shutdown without
					// blocking.
					for range consume.Receive {
						break
					}
					ConsumeErrorC <- consume.Shutdown()
					PeerErrorC <- <-consume.Error
				}()
				wg.Add(1)
				go func() {
					defer wg.Done()
					var once sync.Once
					for n := 0; n < test.Send; n++ {
						// This select case is designed to fill the consumers Receive channel and the IO memory pipe
						// between consumer and producer.  When default triggers these buffers are full and ConsumeStartC
						// is closed to enable consumer to start reading frames while this producer continues to produce
						// to keep the buffers full.
						select {
						case produce.Send <- frames.SendString("/test/peer-shutdown", "hello"):
						default:
							once.Do(func() {
								close(ConsumeStartC)
							})
							for ; n < test.Send; n++ {
								produce.Send <- frames.SendString("/test/peer-shutdown", "hello")
							}
						}
					}
					once.Do(func() {
						close(ConsumeStartC)
					})
					ProduceErrorC <- produce.Shutdown()
					PeerErrorC <- <-produce.Error
				}()
			}
			wg.Wait()
			close(PeerErrorC)
			close(ConsumeErrorC)
			close(ProduceErrorC)
			for err := range PeerErrorC {
				if err != nil {
					chk.ErrorIs(err, io.EOF) // io.EOF is ok
				}
			}
			for err := range ConsumeErrorC {
				chk.NoError(err)
			}
			for err := range ProduceErrorC {
				chk.NoError(err)
			}
		})
	}
}

func TestPeer_ShutdownLocalCausesRemoteEOF(t *testing.T) {
	// When one side of Peer is Shutdown the other receives EOF.
	//
	chk := assert.New(t)
	//
	remote, local := stomp.Pipe()
	local.Start(nil)
	remote.Start(nil)
	//
	send := cap(local.Receive) * 2
	for n := 0; n < send; n++ {
		remote.Send <- frames.SendString("/topic/shutdown-local-causes-remote-eof", "hello")
	}
	for len(local.Receive) != cap(local.Receive) { // let local.Receive fill completely
		time.Sleep(100 * time.Microsecond)
	}
	//
	err := local.Shutdown()
	chk.NoError(err)
	chk.ErrorIs(<-remote.Error, io.EOF)
	err = remote.Shutdown()
	chk.NoError(err)
}
