package app

import (
	"fmt"
	"os"
	"testing"
)

// Event-loop tests feed inputChan directly. Never consume the developer's stdin
// or leave a test input goroutine blocked on an interactive terminal.
func TestMain(m *testing.M) {
	input, err := os.Open(os.DevNull)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	original := os.Stdin
	os.Stdin = input
	code := m.Run()
	os.Stdin = original
	_ = input.Close()
	os.Exit(code)
}
