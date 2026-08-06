// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSyncCommandProblemGuardsCommandAggregatePair(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		command   string
		aggregate string
		want      string
	}{
		{"create order", "order.create", "order", ""},
		{"transition ticket", "kitchenTicket.transition", "kitchenTicket", ""},
		{"bulk ticket transition", "kitchenTicket.transitionAll", "order", ""},
		{"unknown command", "inventory.adjust", "inventory", "unsupported_command_type"},
		{"mismatched aggregate", "order.create", "kitchenTicket", "command_aggregate_mismatch"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := syncCommandProblem(SyncOperation{CommandType: test.command, AggregateType: test.aggregate})
			if got != test.want {
				t.Fatalf("problem = %q; want %q", got, test.want)
			}
		})
	}
}

func TestSyncTimesFallBackWithoutInventingZeroTimestamps(t *testing.T) {
	t.Parallel()

	received := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	operation := SyncOperation{ReceivedAt: received, Mutation: json.RawMessage(`{"payload":{}}`)}
	if got := mutationOccurredAt(operation); !got.Equal(received) {
		t.Fatalf("occurredAt fallback = %s; want %s", got, received)
	}
	if got := operationRecordedAt(operation); !got.Equal(received) {
		t.Fatalf("recordedAt fallback = %s; want %s", got, received)
	}
}
