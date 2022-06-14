package stomp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
)

// Parser accepts a reader and parses STOMP frames.
type Parser struct {
	// b is the io.Reader providing data to the Parser.
	b *bufio.Reader

	// frame is the current STOMP frame that is being read and populated.
	frame Frame

	// err is our parsing error.
	err error
}

// NewParser returns a new STOMP Parser.
func NewParser(r io.Reader) *Parser {
	return &Parser{
		b: bufio.NewReader(r),
	}
}

// Next returns true if parsing can continue.
//
// Next always returns false once Frame has returned an error.
func (p *Parser) Next() bool {
	return p.err == nil
}

// Frame returns the current Frame or an error.
func (p *Parser) Frame() (Frame, error) {
	if p.err != nil {
		return Frame{}, p.err
	}
	p.frame = Frame{
		Headers: Headers{},
	}
	if p.err = p.readCommand(); p.err != nil {
		return Frame{}, p.err
	} else if p.err = p.readHeaders(); p.err != nil {
		return Frame{}, p.err
	} else if p.err = p.readBody(); p.err != nil {
		return Frame{}, p.err
	}
	return p.frame, nil
}

// readCommand reads the frame command from R.
//
// If the error is io.EOF then nothing was read and EOF has occurred cleanly
// between STOMP frames.
func (p *Parser) readCommand() error {
	var c string
	var err error
	// TODO Ignore empty lines between frames (make parser option?)
	for c, err = p.b.ReadString('\n'); c == "\n" && err == nil; c, err = p.b.ReadString('\n') {
	}
	if err != nil {
		// Certain errors represent a clean break between frames if c
		// is empty.  All such errors are coalesced to io.EOF to ease
		// error checking when using the parser.
		asEOF := errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrDeadlineExceeded)
		if c == "" && asEOF {
			return io.EOF
		}
		return fmt.Errorf("%w: reading command: %v", ErrFrame, err.Error())
	}
	//
	p.frame.Command = c[:len(c)-1]
	//
	return nil
}

// readHeaders reads frame headers from R.
func (p *Parser) readHeaders() error {
	var s string
	var colon int
	var ok bool
	var err error
	//
	for {
		s, err = p.b.ReadString('\n')
		if err != nil {
			return fmt.Errorf("%w: reading headers: %v", ErrFrame, err.Error())
		} else if s == "\n" {
			return nil
		} else if colon = strings.IndexRune(s, ':'); colon == -1 {
			return fmt.Errorf("%w: header missing colon: %v", ErrFrame, s[0:len(s)-1])
		}
		// Trim newline and split by index of colon rune.
		// STOMP protocol dictates when headers are repeated the first value wins.
		s = s[0 : len(s)-1]
		header, value := s[0:colon], s[colon+1:]
		if _, ok = p.frame.Headers[header]; !ok {
			p.frame.Headers[header] = value
		}
	}
}

// readBody reads the body from R.
//
// If the current set of headers includes a content-length header then
// the body is read up to that content length.
//
// If there is no content-length header then reading stops at the first null byte.
func (p *Parser) readBody() error {
	var tmp []byte
	var n int
	var err error
	//
	// Check for content-length header.
	if h, ok := p.frame.Headers[HeaderContentLength]; ok {
		if n, err = strconv.Atoi(h); err != nil {
			return fmt.Errorf("%w: invalid content-length: %v", ErrFrame, h)
		}
		n++ // +1 for null byte
		tmp = make([]byte, n)
		// TODO Check read == want?
		if _, err = io.ReadFull(p.b, tmp); err != nil {
			return fmt.Errorf("%w: reading content-length %v byte(s): %v", ErrFrame, n, err.Error())
		}
	} else {
		//
		// Read until null byte, check, and trim.
		if tmp, err = p.b.ReadBytes('\x00'); err != nil {
			return fmt.Errorf("%w: reading until null byte: %v", ErrFrame, err.Error())
		}
		n = len(tmp)
	}
	//
	if n != 1 {
		p.frame.Body = tmp[:n-1]
	}
	//
	return nil
}
