package context

import (
	"sync"
	"testing"
	"time"
)

func TestGetOrCreateCooperationContext(t *testing.T) {
	ue := &AmfUe{}
	first := ue.GetOrCreateCooperationContext()
	second := ue.GetOrCreateCooperationContext()
	if first == nil || first != second {
		t.Fatalf("contexts first=%p second=%p", first, second)
	}
	if first.APContainer == nil || first.APContainer.Reassemblies == nil || first.APContainer.Completed == nil {
		t.Fatal("AP Container maps are not initialized")
	}
}

func TestStoreCompletedAPContainerKeepsNewestEight(t *testing.T) {
	ctx := NewCooperationContext()
	base := time.Unix(1000, 0)
	for id := uint16(1); id <= 9; id++ {
		ctx.StoreCompletedAPContainer(CompletedAPContainer{
			ContainerPayloadID: id,
			Payload:            []byte{byte(id)},
			CompletedAt:        base.Add(time.Duration(id) * time.Second),
		})
	}
	completed := ctx.CompletedAPContainers()
	if len(completed) != 8 {
		t.Fatalf("completed count = %d, want 8", len(completed))
	}
	if _, ok := completed[1]; ok {
		t.Fatal("oldest completed record was not evicted")
	}
	got := completed[9]
	got.Payload[0] = 0xff
	if ctx.CompletedAPContainers()[9].Payload[0] != 9 {
		t.Fatal("completed payload was not returned as an isolated copy")
	}
}

func TestStoreCompletedAPContainerReplacesSameIDWithoutEviction(t *testing.T) {
	ctx := NewCooperationContext()
	base := time.Unix(1000, 0)
	for id := uint16(1); id <= 8; id++ {
		ctx.StoreCompletedAPContainer(CompletedAPContainer{
			ContainerPayloadID: id,
			Payload:            []byte{byte(id)},
			CompletedAt:        base.Add(time.Duration(id) * time.Second),
		})
	}
	ctx.StoreCompletedAPContainer(CompletedAPContainer{
		ContainerPayloadID: 1,
		Payload:            []byte{0xaa},
		CompletedAt:        base.Add(20 * time.Second),
	})

	completed := ctx.CompletedAPContainers()
	if len(completed) != 8 {
		t.Fatalf("completed count = %d, want 8", len(completed))
	}
	if got := completed[1].Payload; len(got) != 1 || got[0] != 0xaa {
		t.Fatalf("replacement payload = %x, want aa", got)
	}
}

func TestStopAPContainerReassemblyTimers(t *testing.T) {
	ue := &AmfUe{}
	ctx := ue.GetOrCreateCooperationContext()
	timer := time.NewTimer(time.Hour)
	key := APContainerReassemblyKey{Direction: APContainerDirectionUL, PayloadID: 1}
	ctx.APContainer.Reassemblies[key] = &APContainerReassemblyState{Timer: timer}

	ue.StopAPContainerReassemblyTimers()

	if len(ctx.APContainer.Reassemblies) != 0 {
		t.Fatalf("reassembly count = %d, want 0", len(ctx.APContainer.Reassemblies))
	}
	if timer.Stop() {
		t.Fatal("timer was still active after cleanup")
	}
}

func TestCooperationContextInitializationAndTimerCleanupConcurrent(t *testing.T) {
	ue := &AmfUe{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			ue.GetOrCreateCooperationContext()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			ue.StopAPContainerReassemblyTimers()
		}
	}()
	wg.Wait()
}
