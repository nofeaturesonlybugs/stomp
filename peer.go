package stomp

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// PeerSignals allows granular control and notification of a Peer's lifecycle.
type PeerSignals struct {
	// Close the StopReader channel to end the goroutine reading from R.
	// AwaitReader is closed when the goroutine is fully stopped.
	StopReader  chan<- Signal
	AwaitReader <-chan Signal

	// Close the Peer.Send channel to end the goroutine writing to W.
	// AwaitSender is closed when the goroutine is fully stopped.
	AwaitSender <-chan Signal

	// AwaitClosed is closed when all goroutines have stopped.
	AwaitClosed <-chan Signal

	// internal handles are necessary so peer can close() them.
	awaitReader chan Signal
	awaitSender chan Signal
	awaitClosed chan Signal
	stopReader  chan Signal
}

// newPeerSignals creates the peer signals type.
func newPeerSignals() PeerSignals {
	awaitReader := make(chan Signal)
	awaitSender := make(chan Signal)
	awaitClosed := make(chan Signal)
	stopReader := make(chan Signal)
	return PeerSignals{
		AwaitReader: awaitReader,
		AwaitSender: awaitSender,
		AwaitClosed: awaitClosed,
		StopReader:  stopReader,
		// internal handles
		awaitReader: awaitReader,
		awaitSender: awaitSender,
		awaitClosed: awaitClosed,
		stopReader:  stopReader,
	}
}

// Pipe creates a pair of peers whose R and W channels are linked by memory pipes.
func Pipe() (Peer, Peer) {
	ar, bw := io.Pipe()
	br, aw := io.Pipe()
	a := Peer{
		R: ar,
		W: aw,
		pipe: pipePeer{
			r: br,
			w: bw,
		},
	}
	b := Peer{
		R: br,
		W: bw,
		pipe: pipePeer{
			r: ar,
			w: aw,
		},
	}
	return a, b
}

// pipePeer contains the remote r+w when a peers are created with a call to Pipe.
//	Peer.R ↔ pipePeer.w  R blocks indefinitely on R.Read(); close pipePeer.w to wake Peer.R.
//	Peer.W ↔ pipePeer.r  Not necessarily needed but provided for completeness.
type pipePeer struct {
	r *io.PipeReader // TODO May be able to remove.
	w *io.PipeWriter
}

// Peer is the remote endpoint of a STOMP peer.
//
// There are two ways to use a Peer.
//
// The first method is to read from and write to R and W directly.  When used in
// this manner you are responsible for the state of the Peer, constructing and parsing
// frames, etc.  When using R and W directly in goroutines you must control the access
// or locking of R and W.
//
// The other way to use a Peer is to call Start to launch internal goroutines
// that will manage R and W.  If Start is called you must yield all access to R and W
// and never use them directly again.  However you will be able to use the Error, Receive,
// and Send channels to receive any error, receive Frames, or send Frames respectively.
type Peer struct {
	// Error, Receive, and Send provide a more convenient way
	// of interacting with the peer in a concurrent environment.
	//
	// These channels will be created in the call to Start; do not
	// set them manually.  After calling Start you must not use
	// R or W directly again.
	//
	// The Error channel will only have one value sent on it, which
	// will be an error or nil, and then the channel is closed.
	//
	// The goroutine reading the Send channel does not end until Send
	// is closed; you must close the Send channel either explicitly
	// with close(Send) or by calling the Shutdown method.
	Error   <-chan error
	Receive <-chan Frame // Frames from the Peer.
	Send    chan<- Frame // Frames to the Peer.

	// Signals allow granular control and notification of the peer's
	// lifecycle after calling Start.
	Signals PeerSignals

	// R and W are the reader and writer connected to the peer.
	//
	// Read from R to obtain STOMP frames from the peer.
	// Write STOMP frames to W to send them to the peer.
	//
	// Any operation on R or W that returns an error represents the
	// peer becoming invalid and should no longer be used.
	//
	// Using R and W directly is mutually exclusive to using
	// the channels above.
	R io.Reader
	W io.Writer

	pipe pipePeer // populated when Peer is created by calling Pipe function.
}

// Close closes both R and W by attempting to convert them to io.Closer.  If
// R and W are the same instance then its close method is called only once.
//
// Close should only be called if you are using R and W directly and did not
// make a call to Start.
func (peer *Peer) Close() error {
	var rc, wc io.Closer
	var ok bool
	var err error
	//
	if rc, ok = peer.R.(io.Closer); ok {
		if e := rc.Close(); e != nil {
			err = fmt.Errorf("%w: closing reader", e)
		}
	}
	if wc, ok = peer.W.(io.Closer); ok && wc != rc {
		if e := wc.Close(); e != nil && err == nil {
			err = fmt.Errorf("%w: closing writer", e)
		}
	}
	//
	return err
}

// EqualRW returns true if R and W are one and the same instance.
func (peer Peer) EqualRW() bool {
	if peer.R == nil || peer.W == nil {
		return false
	}
	r, ok := peer.W.(io.Reader)
	return ok && peer.R == r
}

// Start begins concurrent handling of the Peer and enables communication via
// the Error, Receive, and Send channels.
//
// The goroutines created by Start assume exclusive access to the R and W fields.
//
// The goroutine that writes to W will not end until the Send channel is closed.
// Either close Send explicitly with close(Send) or with a call to Shutdown.
//
// The wait group is optional.
func (peer *Peer) Start(wg *sync.WaitGroup) {
	if wg == nil {
		wg = &sync.WaitGroup{}
	}
	//
	// Data exchange channels.
	// TODO Configurable chan size.
	// 	+ error chan should remain buffer size 1 to fit at most one error.
	//  + Currently tests are implicitly tied to the 128 value below; lowering this value will
	//    probably lock up some tests.
	Send, Receive, Error := make(chan Frame, 128), make(chan Frame, 128), make(chan error, 1)
	peer.Error = Error
	peer.Receive = Receive
	peer.Send = Send
	//
	peer.Signals = newPeerSignals()
	//
	// The error handling for each goroutine is the same and we want it to execute just once.
	var onceError sync.Once
	HandleError := func(err error) {
		fn := func() {
			Error <- err
			close(Error)
		}
		onceError.Do(fn)
	}
	//
	expectReadError := int32(0)
	// expectReadError := false // maybe replace with atomic int // TODO RM
	//
	wg.Add(3)
	go func() {
		defer wg.Done()
		var err error
		if err = peer.reader(Receive); err != nil {
			currExpectReadError := atomic.LoadInt32(&expectReadError)
			if currExpectReadError == 1 && errors.Is(err, ErrFrame) {
				// expected frame error from EOF is ignored
			} else {
				HandleError(fmt.Errorf("stomp: peer reader: %w", err))
			}
		}
		close(Receive)
		close(peer.Signals.awaitReader)
	}()
	go func() {
		defer wg.Done()
		var err error
		if err = peer.writer(Send); err != nil {
			// When R+W are created as pipes this writer may get an io.ErrClosedPipe if the other end closes **our**
			// writer as part of its shutdown and cleanup.
			if peer.pipe.w != nil && errors.Is(err, io.ErrClosedPipe) {
				// NB Optionally we could turn this into io.EOF however this means our W is closed
				//    so we can not send messages and the other end isn't reading anyways so it's
				//    not a problem.  However we can still be reading and thus not exactly in a "complete"
				//    EOF -- hence why we just ignore this quietly.
			} else {
				HandleError(fmt.Errorf("stomp: peer writer: %T %w", err, err))
			}
		}
		// Drain the channel until its empty and closed.
		// NB  Don't move this inside peer.writer or any error returned from peer.writer can't be sent
		//     until this channel is drained.
		for range Send {
		}
		close(peer.Signals.awaitSender)
	}()
	go func() {
		defer wg.Done()
		var err error
		sender, reader, stopReader := peer.Signals.AwaitSender, peer.Signals.AwaitReader, peer.Signals.stopReader
		for sender != nil || reader != nil {
			select {
			case <-reader:
				reader = nil
			case <-sender:
				sender = nil
			case <-stopReader:
				stopReader = nil
				atomic.StoreInt32(&expectReadError, 1)
				// expectReadError = true // TODO RM
				// When R+W are created from io.Pipe our R can block indefinitely until remote W is closed.
				if peer.pipe.w != nil {
					_ = peer.pipe.w.CloseWithError(io.EOF) // docs say always returns nil
					continue                               // pipe's don't have SetReadDeadline so just continue
				}
				// If R supports SetReadDeadline set it to a short time in the future to wake up the reader.
				type ReadDeadliner interface {
					SetReadDeadline(time.Time) error
				}
				if rd, ok := peer.R.(ReadDeadliner); ok {
					if err = rd.SetReadDeadline(time.Now().Add(1 * time.Microsecond)); err != nil {
						HandleError(err)
					}
				}
			}
		}
		if err = peer.Close(); err != nil {
			HandleError(fmt.Errorf("stomp: peer control: %w", err))
		}
		HandleError(nil) // ensure error channel is closed; nil won't overwrite any previous errors
		close(peer.Signals.awaitClosed)
	}()
}

// SendAndShutdown sends frame and calls Shutdown.  This method is only valid after a call
// to Start.
func (peer *Peer) SendAndShutdown(frame Frame) error {
	peer.Send <- frame
	return peer.Shutdown()
}

// Shutdown performs an orderly shutdown of the peer by closing necessary channels
// and waiting for all goroutines to stop.
//
// Shutdown then attempts to read an error from the Error channel.  Any error
// except for io.EOF will be returned.
func (peer *Peer) Shutdown() error {
	// Signal and wait on our sender first so anything queued to go out can do so.
	close(peer.Send)
	<-peer.Signals.AwaitSender
	// Signal and wait on our reader.
	close(peer.Signals.StopReader)
	<-peer.Signals.AwaitReader
	<-peer.Signals.AwaitClosed // TODO Possibly change this to return error from Close() call.
	select {
	case err := <-peer.Error:
		if !errors.Is(err, io.EOF) {
			return err
		}
	default:
	}
	return nil
}

// reader reads from R and pushes Frames into c.
func (peer *Peer) reader(frameC chan<- Frame) error {
	var frame Frame
	var err error
	parser := NewParser(peer.R)
Read:
	for parser.Next() {
		if frame, err = parser.Frame(); err != nil {
			break Read
		}
		// NB Tiered select that gives priority to frameC so any received frame is processed before
		//    potentially exiting due to the other channels.  Note frameC is included in both selects
		//    to handle the case where the first select blocks when frameC is at capacity.
		select {
		case frameC <- frame: // send frame upwards has priority over cancellation
		default:
			select {
			case frameC <- frame:
			case <-peer.Signals.stopReader:
				break Read // TODO frame is lost
			}
		}
	}
	return err
}

// writer pulls Frames from frameC and writes them to W.
func (peer *Peer) writer(frameC <-chan Frame) error {
	var err error
Write:
	for {
		select {
		case frame, open := <-frameC:
			if !open {
				break Write
			}
			if _, err = frame.WriteTo(peer.W); err != nil {
				break Write
			}
		}
	}
	return err
}
