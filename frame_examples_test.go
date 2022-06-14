package stomp_test

import (
	"fmt"
	"strings"

	"github.com/nofeaturesonlybugs/stomp"
)

func ExampleFrame() {
	f := stomp.Frame{
		Command: "COMMAND",
		Headers: stomp.Headers{
			"iam": "sam",
			"foo": "bar",
		},
		Body: []byte("Hello, World!"),
	}
	fmt.Println(strings.TrimRight(f.String(), "\x00"))

	// Output: COMMAND
	// content-length:13
	// foo:bar
	// iam:sam
	//
	// Hello, World!
}
