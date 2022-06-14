package stomp_test

import (
	"bytes"
	"io"
	"net"
	"os"
	"testing"

	"github.com/nofeaturesonlybugs/stomp"
	"github.com/nofeaturesonlybugs/stomp/frames"
	"github.com/stretchr/testify/assert"
)

// ReaderFunc allows a func to be used as io.Reader.
type ReaderFunc func([]byte) (int, error)

// Read implements io.Reader.
func (f ReaderFunc) Read(p []byte) (int, error) {
	return f(p)
}

func TestNetErrClosedBecomesEOFBetweenFrames(t *testing.T) {
	// The target test code for this test is in the Parser when reading a COMMAND.
	// Occasionally when the reader is a net.Conn an error of net.ErrClosed is
	// returned when the remote end is closed.  The parser currently uses io.EOF
	// to semantically signal a clean break from the reader between frames.
	//
	// Therefore when the parser is reading the COMMAND of the next frame if
	// the error is net.ErrClosed we coerce to io.EOF because it is semantically
	// a clean break between frames.
	//
	// This shows up somewhat reliably in certain tests but it is hard to produce
	// a test that creates this condition 100% reliably using net.Listener and
	// net.Conn.  This test uses a mock io.Reader that creates the condition
	// reliably for our code coverage.

	// MakeReader returns an io.Reader that returns one frame and then net.ErrClosed.
	MakeReader := func(asErr error) io.Reader {
		var buf bytes.Buffer
		frames.SendString("/test/net-err-closed-becomes-eof-between-frames", "hello").WriteTo(&buf)
		blob := buf.Bytes()
		fn := func(p []byte) (int, error) {
			if len(blob) == 0 {
				e := &net.OpError{
					Op:  "read",
					Net: "tcp",
					Err: asErr,
				}
				return 0, e
			}
			n := copy(p, blob)
			blob = blob[n:]
			return n, nil
		}
		return ReaderFunc(fn)
	}

	type ErrTest struct {
		Name string
		As   error
	}
	tests := []ErrTest{
		{Name: "net.ErrClosed", As: net.ErrClosed},
		{Name: "os.ErrDeadlineExceeded", As: os.ErrDeadlineExceeded},
	}

	// Within the Parser p.Frame() should return io.EOF when this occurs.
	for _, test := range tests {
		t.Run("parser "+test.Name, func(t *testing.T) {
			chk := assert.New(t)

			p := stomp.NewParser(MakeReader(test.As))
			f, err := p.Frame()
			chk.NoError(err)
			chk.Equal(stomp.CommandSend, f.Command)
			chk.Equal([]byte("hello"), f.Body)

			f, err = p.Frame()
			chk.ErrorIs(err, io.EOF)
			chk.Equal("", f.Command)
			chk.Nil(f.Body)
		})
	}

	// Within the Peer the Shutdown method should return nil since io.EOF
	// during shutdown is ignored -- i.e. a clean break between frames during
	// shutdown is desired so don't make an error out of it.
	for _, test := range tests {
		t.Run("peer-shutdown "+test.Name, func(t *testing.T) {
			chk := assert.New(t)

			p := stomp.Peer{
				R: MakeReader(test.As),
				W: nil,
			}
			p.Start(nil)

			f := <-p.Receive
			chk.Equal(stomp.CommandSend, f.Command)
			chk.Equal([]byte("hello"), f.Body)

			err := p.Shutdown()
			chk.NoError(err)
		})
	}
}
