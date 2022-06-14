package stomp

import (
	"bytes"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
)

func TestParser_Command(t *testing.T) {
	type ParserCommandTest struct {
		Name   string
		Bytes  []byte
		Expect string
		Error  error
	}
	tests := []ParserCommandTest{
		{
			Name:   "good",
			Bytes:  []byte("COMMAND\n"),
			Expect: "COMMAND",
		},
		{
			Name:  "no-newline",
			Bytes: []byte("COMMAND"),
			Error: ErrFrame,
		},
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			chk := assert.New(t)
			b := bytes.NewBuffer(test.Bytes)
			p := NewParser(b)
			err := p.readCommand()
			chk.Equal(test.Expect, p.frame.Command)
			chk.ErrorIs(err, test.Error)
		})
		t.Run("byte-reader "+test.Name, func(t *testing.T) {
			chk := assert.New(t)
			b := bytes.NewBuffer(test.Bytes)
			p := NewParser(iotest.OneByteReader(b))
			err := p.readCommand()
			chk.Equal(test.Expect, p.frame.Command)
			chk.ErrorIs(err, test.Error)
		})
		t.Run("half-reader "+test.Name, func(t *testing.T) {
			chk := assert.New(t)
			b := bytes.NewBuffer(test.Bytes)
			p := NewParser(iotest.HalfReader(b))
			err := p.readCommand()
			chk.Equal(test.Expect, p.frame.Command)
			chk.ErrorIs(err, test.Error)
		})
	}
}

func TestParser_Headers(t *testing.T) {
	type ParserHeadersTest struct {
		Name   string
		Bytes  []byte
		Expect Headers
		Error  error
	}
	tests := []ParserHeadersTest{
		{
			Name:  "one",
			Bytes: []byte("header1:value1\n\n"),
			Expect: Headers{
				"header1": "value1",
			},
		},
		{
			Name:  "two",
			Bytes: []byte("header1:value1\nheader2:value2\n\n"),
			Expect: Headers{
				"header1": "value1",
				"header2": "value2",
			},
		},
		{
			Name:   "no-newline",
			Bytes:  []byte("header1:value1"),
			Expect: Headers{},
			Error:  ErrFrame,
		},
		{
			Name:  "err-invalid-header",
			Bytes: []byte("header1:value1\nheader2value2\n\n"),
			Expect: Headers{
				"header1": "value1",
			},
			Error: ErrFrame,
		},
		{
			Name:  "err-invalid-header-2",
			Bytes: []byte("header1:value1\nheader2:value2\ninvalid\n\n"),
			Expect: Headers{
				"header1": "value1",
				"header2": "value2",
			},
			Error: ErrFrame,
		},
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			chk := assert.New(t)
			b := bytes.NewBuffer(test.Bytes)
			p := NewParser(b)
			p.frame.Headers = Headers{} // p.readHeaders expects the map to exist.
			err := p.readHeaders()
			chk.Equal(test.Expect, p.frame.Headers)
			chk.ErrorIs(err, test.Error)
		})
		t.Run("byte-reader "+test.Name, func(t *testing.T) {
			chk := assert.New(t)
			b := bytes.NewBuffer(test.Bytes)
			p := NewParser(iotest.OneByteReader(b))
			p.frame.Headers = Headers{} // p.readHeaders expects the map to exist.
			err := p.readHeaders()
			chk.Equal(test.Expect, p.frame.Headers)
			chk.ErrorIs(err, test.Error)
		})
		t.Run("half-reader "+test.Name, func(t *testing.T) {
			chk := assert.New(t)
			b := bytes.NewBuffer(test.Bytes)
			p := NewParser(iotest.HalfReader(b))
			p.frame.Headers = Headers{} // p.readHeaders expects the map to exist.
			err := p.readHeaders()
			chk.Equal(test.Expect, p.frame.Headers)
			chk.ErrorIs(err, test.Error)
		})
	}
}

func TestParser_Body(t *testing.T) {
	type ParserBodyTest struct {
		Name    string
		Headers Headers
		Bytes   []byte
		Expect  []byte
		Error   error
	}
	tests := []ParserBodyTest{
		{
			Name:   "no content-length",
			Bytes:  []byte("Hello\nWorld!\x00"),
			Expect: []byte("Hello\nWorld!"),
		},
		{
			Name: "content-length",
			Headers: Headers{
				"content-length": "12",
			},
			Bytes:  []byte("Hello\x00World!\x00"),
			Expect: []byte("Hello\x00World!"),
		},
		{
			Name: "invalid length",
			Headers: Headers{
				"content-length": "asdf",
			},
			Bytes: []byte("Hello\x00World!\x00"),
			Error: ErrFrame,
		},
		{
			Name:  "no null",
			Bytes: []byte("Hello World!"),
			Error: ErrFrame,
		},
		{
			Name: "too short",
			Headers: Headers{
				"content-length": "72",
			},
			Bytes: []byte("Hello\x00World!\x00"),
			Error: ErrFrame,
		},
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			chk := assert.New(t)
			b := bytes.NewBuffer(test.Bytes)
			p := NewParser(b)
			p.frame = Frame{
				Headers: test.Headers,
			}
			err := p.readBody()
			chk.Equal(test.Expect, p.frame.Body)
			chk.ErrorIs(err, test.Error)
		})
		t.Run("byte-reader "+test.Name, func(t *testing.T) {
			chk := assert.New(t)
			b := bytes.NewBuffer(test.Bytes)
			p := NewParser(iotest.OneByteReader(b))
			p.frame = Frame{
				Headers: test.Headers,
			}
			err := p.readBody()
			chk.Equal(test.Expect, p.frame.Body)
			chk.ErrorIs(err, test.Error)
		})
		t.Run("half-reader "+test.Name, func(t *testing.T) {
			chk := assert.New(t)
			b := bytes.NewBuffer(test.Bytes)
			p := NewParser(iotest.HalfReader(b))
			p.frame = Frame{
				Headers: test.Headers,
			}
			err := p.readBody()
			chk.Equal(test.Expect, p.frame.Body)
			chk.ErrorIs(err, test.Error)
		})
	}
}

func BenchmarkParser_Headers(b *testing.B) {
	type ParserHeadersBench struct {
		Name   string
		Bytes  []byte
		Expect Headers
		Error  error
	}
	tests := []ParserHeadersBench{
		{
			Name:  "one",
			Bytes: []byte("header1:value1\n\n"),
			Expect: Headers{
				"header1": "value1",
			},
		},
		{
			Name:  "two",
			Bytes: []byte("header1:value1\nheader2:value2\n\n"),
			Expect: Headers{
				"header1": "value1",
				"header2": "value2",
			},
		},
		{
			Name:  "err-invalid-header",
			Bytes: []byte("header1:value1\nheader2value2\n\n"),
			Expect: Headers{
				"header1": "value1",
			},
			Error: ErrFrame,
		},
		{
			Name:  "err-invalid-header-2",
			Bytes: []byte("header1:value1\nheader2:value2\ninvalid\n\n"),
			Expect: Headers{
				"header1": "value1",
				"header2": "value2",
			},
			Error: ErrFrame,
		},
	}
	for _, test := range tests {
		b.Run(test.Name, func(b *testing.B) {
			buf := bytes.NewBuffer(test.Bytes)
			p := NewParser(buf)
			p.frame.Headers = Headers{}
			for n := 0; n < b.N; n++ {
				_ = p.readHeaders()
				_, _ = buf.Write(test.Bytes)
				p.frame.Headers = Headers{}
			}
		})
	}
}
