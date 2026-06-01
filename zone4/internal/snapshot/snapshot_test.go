package snapshot_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"archgraph/zone4/internal/deltalog"
	"archgraph/zone4/internal/graphdb"
	"archgraph/zone4/internal/snapshot"
	"archgraph/zone4/schema"
)

func TestSnapshot_CreateAndRestore(t *testing.T) {
	ctx := context.Background()
	store, err := graphdb.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	// 1. Setup a few entries in entities/relationships & delta_log
	tx, err := store.DB().Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	e1 := &schema.Entity{
		ID:            "ent_1",
		Type:          schema.EntityService,
		CanonicalName: "service-1",
		Namespace:     "default",
		Confidence:    1.0,
		IsActive:      true,
		ValidFrom:     time.Now().Add(-10 * time.Minute),
	}
	if err := graphdb.InsertEntity(tx, e1); err != nil {
		t.Fatalf("insert entity: %v", err)
	}

	afterBytes, _ := json.Marshal(e1)
	_, err = deltalog.Append(tx, &deltalog.Entry{
		TransactionID: "txn_1",
		MutationType:  deltalog.MutEntityCreated,
		EntityID:      e1.ID,
		AfterState:    afterBytes,
		OccurredAt:    time.Now().Add(-10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("log append: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// 2. Take a snapshot
	snapStore := snapshot.New(store.DB())
	snapTime := time.Now().Add(-5 * time.Minute)
	meta, err := snapStore.CreateSnapshot(ctx, "snap_test_1", snapTime)
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	if meta.NodeCount != 1 || meta.ActiveServices != 1 {
		t.Errorf("unexpected snapshot stats: %+v", meta)
	}

	// 3. Mutate after the snapshot (create e2, update e1)
	tx2, err := store.DB().Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	e1.Version = 2
	e1.Confidence = 0.8
	if err := graphdb.UpdateEntity(tx2, e1, 1); err != nil {
		t.Fatalf("update entity: %v", err)
	}

	e2 := &schema.Entity{
		ID:            "ent_2",
		Type:          schema.EntityService,
		CanonicalName: "service-2",
		Namespace:     "default",
		Confidence:    0.9,
		IsActive:      true,
		ValidFrom:     time.Now(),
	}
	if err := graphdb.InsertEntity(tx2, e2); err != nil {
		t.Fatalf("insert entity 2: %v", err)
	}

	after1, _ := json.Marshal(e1)
	_, _ = deltalog.Append(tx2, &deltalog.Entry{
		TransactionID: "txn_2",
		MutationType:  deltalog.MutEntityUpdated,
		EntityID:      e1.ID,
		AfterState:    after1,
		OccurredAt:    time.Now(),
	})

	after2, _ := json.Marshal(e2)
	_, _ = deltalog.Append(tx2, &deltalog.Entry{
		TransactionID: "txn_2",
		MutationType:  deltalog.MutEntityCreated,
		EntityID:      e2.ID,
		AfterState:    after2,
		OccurredAt:    time.Now(),
	})

	if err := tx2.Commit(); err != nil {
		t.Fatalf("commit 2: %v", err)
	}

	// 4. Restore as of snapTime (should only contain service-1 at v1, confidence 1.0)
	state1, err := snapStore.RestoreGraph(ctx, snapTime)
	if err != nil {
		t.Fatalf("restore graph at snapTime: %v", err)
	}
	if len(state1.Entities) != 1 || state1.Entities[0].ID != "ent_1" {
		t.Fatalf("expected 1 entity (ent_1), got %d: %+v", len(state1.Entities), state1.Entities)
	}
	if state1.Entities[0].Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %v", state1.Entities[0].Confidence)
	}

	// 5. Restore as of now (should contain both, service-1 at confidence 0.8)
	state2, err := snapStore.RestoreGraph(ctx, time.Now())
	if err != nil {
		t.Fatalf("restore graph now: %v", err)
	}
	if len(state2.Entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(state2.Entities))
	}

	var restoredE1 *schema.Entity
	for _, ent := range state2.Entities {
		if ent.ID == "ent_1" {
			restoredE1 = ent
		}
	}
	if restoredE1 == nil || restoredE1.Confidence != 0.8 {
		t.Errorf("expected ent_1 confidence 0.8, got %v", restoredE1)
	}
}
