package main

import (
	"io"
	"log"
	"strings"
	"testing"
	"time"
)

func TestWatchStdinForShutdown(t *testing.T) {
	shutdown := watchStdinForShutdown(strings.NewReader("ignored\nshutdown\n"), log.New(io.Discard, "", 0))
	select {
	case <-shutdown:
	case <-time.After(time.Second):
		t.Fatal("shutdown control message was not observed")
	}
}

func TestWatchStdinIgnoresEOF(t *testing.T) {
	shutdown := watchStdinForShutdown(strings.NewReader(""), log.New(io.Discard, "", 0))
	select {
	case <-shutdown:
		t.Fatal("EOF must not request shutdown")
	case <-time.After(10 * time.Millisecond):
	}
}
