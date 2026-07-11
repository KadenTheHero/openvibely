package contracts

import (
	"context"
	"testing"
)

func TestTransportScopeContextRoundTrip(t *testing.T) {
	ctx := WithTransportScope(context.Background(), "chat:project:project-1")
	if got := TransportScopeFromContext(ctx); got != "chat:project:project-1" {
		t.Fatalf("TransportScopeFromContext = %q", got)
	}
}

func TestTransportScopeContextDefault(t *testing.T) {
	if got := TransportScopeFromContext(context.Background()); got != "" {
		t.Fatalf("TransportScopeFromContext default = %q", got)
	}
}
