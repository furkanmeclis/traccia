package outbox

import (
	"context"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"testing"
)

func TestConsumerRegistryRejectsInvalidBindings(t *testing.T) {
	handler := func(context.Context, events.Event) error { return nil }
	for _, bindings := range [][]Consumer{{{Name: "activity", Version: 1}}, {{Name: "activity", Version: 0, Handle: handler}}, {{Name: "activity", Version: 1, Handle: handler}, {Name: "activity", Version: 1, Handle: handler}}} {
		if _, err := NewRegistry(bindings...); err == nil {
			t.Fatal("invalid consumer registry accepted")
		}
	}
	if _, err := NewRegistry(Consumer{Name: "activity", Version: 1, Handle: handler}, Consumer{Name: "activity", Version: 2, Handle: handler}); err != nil {
		t.Fatal(err)
	}
}
