package stream

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"notifier/internal/model"
)

const validPayload = `{"event_id":"550e8400-e29b-41d4-a716-446655440000","id":123,"source":"mock",
	"deal_type":"rent","country":"BY","city":"Minsk","rooms":1,"price":380,"currency":"USD",
	"price_usd":380,"address":null,"url":"https://example.com/123",
	"created_at":"2026-10-28T10:00:00Z"}`

// fakeRedis — сценарный двойник Redis: первую пачку сообщений выдаёт XREADGROUP,
// дальше пусто. Подтверждения и dead-letter пишет в логи для проверок.
type fakeRedis struct {
	mu        sync.Mutex
	groupErr  error
	msgs      []redis.XMessage
	acked     []string
	dead      []redis.XAddArgs
	xaddErr   error
	readCalls int
}

func (f *fakeRedis) XGroupCreateMkStream(ctx context.Context, stream, group, start string) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	if f.groupErr != nil {
		cmd.SetErr(f.groupErr)
	} else {
		cmd.SetVal("OK")
	}
	return cmd
}

func (f *fakeRedis) XReadGroup(ctx context.Context, a *redis.XReadGroupArgs) *redis.XStreamSliceCmd {
	f.mu.Lock()
	f.readCalls++
	msgs := f.msgs
	f.msgs = nil
	f.mu.Unlock()
	cmd := redis.NewXStreamSliceCmd(ctx, a)
	if len(msgs) > 0 {
		cmd.SetVal([]redis.XStream{{Stream: StreamName, Messages: msgs}})
		return cmd
	}
	if ctx.Err() != nil {
		cmd.SetErr(ctx.Err())
		return cmd
	}
	cmd.SetErr(redis.Nil) // имитация истечения Block
	return cmd
}

func (f *fakeRedis) XAutoClaim(ctx context.Context, a *redis.XAutoClaimArgs) *redis.XAutoClaimCmd {
	cmd := redis.NewXAutoClaimCmd(ctx, a)
	cmd.SetVal(nil, "0-0")
	return cmd
}

func (f *fakeRedis) XAck(ctx context.Context, stream, group string, ids ...string) *redis.IntCmd {
	f.mu.Lock()
	f.acked = append(f.acked, ids...)
	f.mu.Unlock()
	cmd := redis.NewIntCmd(ctx, stream, group, ids)
	cmd.SetVal(int64(len(ids)))
	return cmd
}

func (f *fakeRedis) XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd {
	f.mu.Lock()
	f.dead = append(f.dead, *a)
	f.mu.Unlock()
	cmd := redis.NewStringCmd(ctx, a)
	if f.xaddErr != nil {
		cmd.SetErr(f.xaddErr)
	} else {
		cmd.SetVal("dead-1")
	}
	return cmd
}

func (f *fakeRedis) ackedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.acked...)
}

func (f *fakeRedis) deadCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.dead)
}

func (f *fakeRedis) deadValues() []redis.XAddArgs {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]redis.XAddArgs(nil), f.dead...)
}

// runConsumer запускает консьюмер, ждёт готовности условия и останавливает его.
func runConsumer(t *testing.T, rdb *fakeRedis, handle Handler, ready func() bool) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- NewConsumer(rdb, "test-1", handle).Run(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for !ready() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not stop after cancel")
		return nil
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestConsumerDeliversAndAcks(t *testing.T) {
	rdb := &fakeRedis{msgs: []redis.XMessage{
		{ID: "1-1", Values: map[string]any{"data": validPayload}},
	}}

	handled := make(chan model.Event, 1)
	err := runConsumer(t, rdb, func(_ context.Context, e model.Event) {
		handled <- e
	}, func() bool { return len(rdb.ackedIDs()) == 1 })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	select {
	case e := <-handled:
		if e.ID != 123 || e.EventID == "" {
			t.Errorf("unexpected event: %+v", e)
		}
	default:
		t.Fatal("handler not called")
	}
	if got := rdb.ackedIDs(); len(got) != 1 || got[0] != "1-1" {
		t.Errorf("acked = %v, want [1-1]", got)
	}
	if rdb.deadCount() != 0 {
		t.Errorf("dead-letter = %d, want 0", rdb.deadCount())
	}
}

func TestConsumerDeadLetterOnBadPayload(t *testing.T) {
	rdb := &fakeRedis{msgs: []redis.XMessage{
		{ID: "2-1", Values: map[string]any{"data": `{oops`}},
	}}

	handlerCalled := false
	err := runConsumer(t, rdb, func(context.Context, model.Event) { handlerCalled = true },
		func() bool { return rdb.deadCount() == 1 && len(rdb.ackedIDs()) == 1 })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if handlerCalled {
		t.Error("handler must not be called for bad payload")
	}
	if got := rdb.ackedIDs(); len(got) != 1 || got[0] != "2-1" {
		t.Errorf("acked = %v, want [2-1]", got)
	}
	dead := rdb.deadValues()
	if len(dead) != 1 || dead[0].Stream != DeadStream {
		t.Fatalf("dead = %+v", dead)
	}
	vals, _ := dead[0].Values.(map[string]any)
	if vals["reason"] == "" || vals["data"] != `{oops` {
		t.Errorf("dead values = %v", vals)
	}
}

func TestConsumerDeadLetterOnMissingData(t *testing.T) {
	rdb := &fakeRedis{msgs: []redis.XMessage{
		{ID: "3-1", Values: map[string]any{"other": "x"}},
	}}
	err := runConsumer(t, rdb, func(context.Context, model.Event) {},
		func() bool { return rdb.deadCount() == 1 })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestConsumerKeepsPendingWhenDeadLetterFails(t *testing.T) {
	rdb := &fakeRedis{
		msgs:    []redis.XMessage{{ID: "4-1", Values: map[string]any{"data": `{oops`}}},
		xaddErr: errors.New("redis unavailable"),
	}
	err := runConsumer(t, rdb, func(context.Context, model.Event) {},
		func() bool { return true }) // сразу останавливаем
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// XADD не прошёл — подтверждать нельзя, запись должна остаться в pending.
	if got := rdb.ackedIDs(); len(got) != 0 {
		t.Errorf("acked = %v, want none (dead-letter write failed)", got)
	}
}

func TestConsumerToleratesExistingGroup(t *testing.T) {
	rdb := &fakeRedis{
		groupErr: errors.New("BUSYGROUP Consumer Group name already exists"),
		msgs:     []redis.XMessage{{ID: "5-1", Values: map[string]any{"data": validPayload}}},
	}
	err := runConsumer(t, rdb, func(context.Context, model.Event) {},
		func() bool { return len(rdb.ackedIDs()) == 1 })
	if err != nil {
		t.Fatalf("Run must tolerate BUSYGROUP: %v", err)
	}
}

func TestConsumerGroupCreateFailureIsFatal(t *testing.T) {
	rdb := &fakeRedis{groupErr: errors.New("WRONGTYPE bad kind")}
	err := runConsumer(t, rdb, func(context.Context, model.Event) {}, func() bool { return true })
	if err == nil {
		t.Fatal("group create failure must be returned as error")
	}
}

func TestConsumerName(t *testing.T) {
	if ConsumerName() == "" {
		t.Fatal("consumer name must not be empty")
	}
}
