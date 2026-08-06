// SPDX-License-Identifier: AGPL-3.0-only

package model

import (
	"strings"
	"testing"
	"time"
)

func TestNewUUIDv7HasCorrectVersionVariantAndOrder(t *testing.T) {
	t.Parallel()
	previous := ""
	seen := make(map[string]struct{})
	for index := 0; index < 2_000; index++ {
		identifier := NewUUIDv7()
		if !IsUUIDv7(identifier) {
			t.Fatalf("generated identifier is not UUIDv7: %q", identifier)
		}
		if identifier[14] != '7' || !strings.ContainsRune("89ab", rune(identifier[19])) {
			t.Fatalf("incorrect version or variant bits in %q", identifier)
		}
		if previous != "" && identifier <= previous {
			t.Fatalf("UUIDv7 sequence is not increasing: %q then %q", previous, identifier)
		}
		if _, duplicate := seen[identifier]; duplicate {
			t.Fatalf("duplicate UUIDv7 generated: %q", identifier)
		}
		seen[identifier] = struct{}{}
		previous = identifier
	}
}

func TestMutationEnvelopeRequiresUUIDv7(t *testing.T) {
	t.Parallel()
	envelope := MutationEnvelope{
		ID: NewUUID(), TenantID: "tenant", OutletID: "outlet", DeviceID: "device", ActorID: "actor",
		OccurredAt: testTime, Source: "test", SchemaVersion: CurrentSchemaVersion,
		IdempotencyKey: "idempotency-key", Payload: []byte(`{}`),
	}
	if err := envelope.Validate(); err == nil || !strings.Contains(err.Error(), "UUIDv7") {
		t.Fatalf("expected UUIDv7 validation error, got %v", err)
	}
}

func TestNewOrderValidatesOfflineStationTicketIdentities(t *testing.T) {
	t.Parallel()
	lineID := NewUUIDv7()
	order := NewOrder{
		ID: NewUUIDv7(), Type: OrderTypeTakeaway, PlacedAt: testTime,
		Lines:            []OrderLine{{ID: lineID, Name: "Biryani", Quantity: 1, StationID: "hot"}},
		StationTicketIDs: map[string]string{"hot": NewUUIDv7()},
	}
	if err := order.Validate(); err != nil {
		t.Fatalf("valid offline station identities were rejected: %v", err)
	}

	order.StationTicketIDs = map[string]string{"beverage": NewUUIDv7()}
	if err := order.Validate(); err == nil || !strings.Contains(err.Error(), "unknown station") {
		t.Fatalf("unknown station ticket identity error = %v", err)
	}

	order.StationTicketIDs = map[string]string{"hot": lineID}
	if err := order.Validate(); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate ticket identity error = %v", err)
	}
}

var testTime = time.Unix(1_700_000_000, 0).UTC()
