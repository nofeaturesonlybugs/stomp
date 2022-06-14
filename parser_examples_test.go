package stomp_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/nofeaturesonlybugs/stomp"
)

func ExampleParser() {
	// Two sample frames.
	frames := &bytes.Buffer{}
	frames.WriteString(strings.TrimSpace(`
COMMAND
foo:bar
hello:world

frame #1
	`))
	frames.WriteString("\x00")
	frames.WriteString(strings.TrimSpace(`
COMMAND
key:value
biz:baz

frame #2
	`))
	frames.WriteString("\x00")

	p := stomp.NewParser(frames)
	for p.Next() {
		f, err := p.Frame()
		if err != nil {
			if errors.Is(err, io.EOF) {
				// When err is io.EOF p.Next() will return false and loop ends gracefully.
				continue
			}
			// Any other error indicates an invalid frame and ends parsing.
			fmt.Println(err)
			continue
		}
		fmt.Println(strings.TrimRight(f.String(), "\x00"))
	}

	// Output: COMMAND
	// content-length:8
	// foo:bar
	// hello:world
	//
	// frame #1
	// COMMAND
	// content-length:8
	// biz:baz
	// key:value
	//
	// frame #2
}

func ExampleParser_invalidHeader() {
	// Two sample frames.
	frames := &bytes.Buffer{}
	frames.WriteString(strings.TrimSpace(`
COMMAND
foo:bar
hello:world

frame #1
	`))
	frames.WriteString("\x00")
	frames.WriteString(strings.TrimSpace(`
COMMAND
key:value
invalid-header-value

frame #2
	`))
	frames.WriteString("\x00")

	p := stomp.NewParser(frames)
	n := 0
	for p.Next() {
		n++
		_, err := p.Frame()
		if err != nil {
			if errors.Is(err, io.EOF) {
				// When err is io.EOF p.Next() will return false and loop ends gracefully.
				continue
			}
			// Any other error indicates an invalid frame and ends parsing.
			fmt.Println(err)
			continue
		}
		fmt.Printf("Frame #%v ok\n", n)
	}

	// Output: Frame #1 ok
	// stomp: invalid frame: header missing colon: invalid-header-value
}

func ExampleParser_unexpectedEOF() {
	// Two sample frames.
	frames := &bytes.Buffer{}
	frames.WriteString(strings.TrimSpace(`
COMMAND
foo:bar
hello:world

frame #1
	`))
	frames.WriteString("\x00")
	frames.WriteString(strings.TrimSpace(`
COMMAND
key:value

this frame does not have the null byte and results in unexpected EOF
	`))

	p := stomp.NewParser(frames)
	n := 0
	for p.Next() {
		n++
		_, err := p.Frame()
		if err != nil {
			if errors.Is(err, io.EOF) {
				// When err is io.EOF p.Next() will return false and loop ends gracefully.
				continue
			}
			// Any other error indicates an invalid frame and ends parsing.
			fmt.Println(err)
			continue
		}
		fmt.Printf("Frame #%v ok\n", n)
	}

	// Output: Frame #1 ok
	// stomp: invalid frame: reading until null byte: EOF
}
