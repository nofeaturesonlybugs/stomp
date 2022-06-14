package stomp

import (
	"fmt"
	"io"
	"strings"
)

// Frame is a STOMP frame.
type Frame struct {
	Command string
	Headers Headers
	Body    []byte
}

// Empty returns true if the frame is empty.  An empty frame has no command,
// no headers, and a zero-length body.
func (f Frame) Empty() bool {
	return f.Command == "" && len(f.Headers) == 0 && len(f.Body) == 0
}

// String returns the STOMP frame as a string.
func (f Frame) String() string {
	s := &strings.Builder{}
	if _, err := f.WriteTo(s); err != nil {
		return ""
	}
	return s.String()
}

// WriteTo writes data to w until there's no more data to write or when an error occurs.
// The return value n is the number of bytes written. Any error encountered during the
// write is also returned.
func (f Frame) WriteTo(w io.Writer) (int64, error) {
	var total, n int
	var err error
	//
	n, err = io.WriteString(w, f.Command+"\n")
	total += n
	if err != nil {
		return int64(total), err
	}
	//
	if contentLength := len(f.Body); contentLength > 0 {
		n, err = fmt.Fprintf(w, "%v:%v\n", HeaderContentLength, contentLength)
		total += n
		if err != nil {
			return int64(total), err
		}
	}
	//
	for _, header := range f.Headers.SortedKeys() {
		value := f.Headers[header]
		n, err = io.WriteString(w, header+":"+value+"\n")
		total += n
		if err != nil {
			return int64(total), err
		}
	}
	n, err = io.WriteString(w, "\n")
	total += n
	if err != nil {
		return int64(total), err
	}
	//
	n, err = w.Write(f.Body)
	total += n
	if err != nil {
		return int64(total), err
	}
	n, err = w.Write([]byte{0x00})
	total += n
	if err != nil {
		return int64(total), err
	}
	//
	return int64(total), nil
}
