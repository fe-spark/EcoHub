package handler

import (
	"errors"
	"strings"
	"testing"
)

func TestOpenSSE_WritesFrames(t *testing.T) {
	c, w := testContext("GET", "/sse")
	stream, ok := openSSE(c)
	if !ok {
		t.Fatal("openSSE should succeed on the test response writer")
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type: %s", ct)
	}
	if err := stream.SendData(map[string]string{"event": "start"}); err != nil {
		t.Fatal(err)
	}
	if err := stream.SendEvent("progress", []int{1}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Comment("ping"); err != nil {
		t.Fatal(err)
	}

	body := w.Body.String()
	for _, want := range []string{
		`data: {"event":"start"}`,
		"event: progress\ndata: [1]",
		": ping",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
}

func TestSSEWriter_MarshalErrorIsNotWriteError(t *testing.T) {
	c, _ := testContext("GET", "/sse")
	stream, ok := openSSE(c)
	if !ok {
		t.Fatal("openSSE should succeed")
	}
	err := stream.SendData(make(chan int))
	if err == nil || errors.Is(err, errSSEWrite) {
		t.Fatalf("marshal error should stay distinct, got %v", err)
	}
}
