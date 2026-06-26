package service

import "testing"

func TestSendMessageMarkerIsRuntimeToolOnly(t *testing.T) {
	output := `[SEND_MESSAGE]{"target":"email:person@example.com","message":"hello"}[/SEND_MESSAGE]`
	requests := ParseTaskCreations(output)
	if len(requests) != 0 {
		t.Fatalf("send_message marker fallback should not be parsed as a task action: %#v", requests)
	}
}
