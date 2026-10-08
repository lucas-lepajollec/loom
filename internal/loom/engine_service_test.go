package loom

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type serviceFixture struct {
	mu      sync.Mutex
	models  []string
	loads   []string
	clock   time.Time
	unloads int
	policy  engineServicePolicy
	service *engineService
}

func newServiceFixture(max int) *serviceFixture {
	f := &serviceFixture{models: []string{"a"}, clock: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC), policy: engineServicePolicy{Idle: 30, Wait: 300, Grace: 15, Max: max}}
	f.service = newEngineService(func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.clock }, func() engineServicePolicy { f.mu.Lock(); defer f.mu.Unlock(); return f.policy }, func() ([]string, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return append([]string{}, f.models...), nil
	}, func() error { f.mu.Lock(); defer f.mu.Unlock(); f.unloads++; f.models = nil; return nil })
	return f
}
func (f *serviceFixture) load(model string) func() error {
	return func() error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.loads = append(f.loads, model)
		if f.policy.Max == 1 {
			f.models = []string{model}
		} else {
			f.models = append(f.models, model)
		}
		return nil
	}
}
func (f *serviceFixture) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clock = f.clock.Add(d)
}
func waitServiceCondition(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !check() {
		select {
		case <-deadline.C:
			t.Fatal("service condition timed out")
		case <-tick.C:
		}
	}
}
func waitServiceQueue(t *testing.T, s *engineService, n int) {
	t.Helper()
	waitServiceCondition(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return len(s.queue) == n })
}
func requestService(t *testing.T, f *serviceFixture, model string) <-chan func() {
	t.Helper()
	result := make(chan func(), 1)
	go func() {
		release, err := f.service.acquire(context.Background(), model, "interactive", f.load(model))
		if err != nil {
			t.Error(err)
		}
		result <- release
	}()
	return result
}
func serviceRelease(t *testing.T, ch <-chan func()) func() {
	t.Helper()
	select {
	case release := <-ch:
		if release == nil {
			t.Fatal("missing reservation")
		}
		return release
	case <-time.After(3 * time.Second):
		t.Fatal("missing grant")
		return nil
	}
}

func TestEngineServiceDrainFIFOAndBatch(t *testing.T) {
	f := newServiceFixture(1)
	old, err := f.service.acquire(context.Background(), "a", "interactive", f.load("a"))
	if err != nil {
		t.Fatal(err)
	}
	b1 := requestService(t, f, "b")
	waitServiceQueue(t, f.service, 1)
	c := requestService(t, f, "c")
	waitServiceQueue(t, f.service, 2)
	b2 := requestService(t, f, "b")
	waitServiceQueue(t, f.service, 3)
	a := requestService(t, f, "a")
	waitServiceQueue(t, f.service, 4)
	f.mu.Lock()
	if len(f.loads) != 0 {
		t.Error("swapped before stream drained")
	}
	f.mu.Unlock()
	old()
	rb1, rb2 := serviceRelease(t, b1), serviceRelease(t, b2)
	select {
	case <-c:
		t.Fatal("c jumped ahead of the b batch")
	default:
	}
	rb1()
	select {
	case <-c:
		t.Fatal("c evicted an in-flight b")
	default:
	}
	rb2()
	serviceRelease(t, c)()
	serviceRelease(t, a)()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.loads) != 3 || f.loads[0] != "b" || f.loads[1] != "c" || f.loads[2] != "a" {
		t.Fatalf("load order %v", f.loads)
	}
}
func TestEngineServiceTimeoutAndCancellation(t *testing.T) {
	f := newServiceFixture(1)
	old, err := f.service.acquire(context.Background(), "a", "interactive", f.load("a"))
	if err != nil {
		t.Fatal(err)
	}
	defer old()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := f.service.acquire(ctx, "b", "interactive", f.load("b")); !errors.Is(err, errEngineBusy) {
		t.Fatalf("timeout: %v", err)
	}
	waitServiceQueue(t, f.service, 0)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.loads) != 0 {
		t.Fatal("expired request loaded its model")
	}
}
func TestEngineServiceBackgroundAndGrace(t *testing.T) {
	f := newServiceFixture(1)
	interactive, err := f.service.acquire(context.Background(), "a", "interactive", f.load("a"))
	if err != nil {
		t.Fatal(err)
	}
	background, err := f.service.acquire(context.Background(), "a", "background", f.load("a"))
	if err != nil {
		t.Fatal(err)
	}
	background()
	if _, err := f.service.acquire(context.Background(), "b", "background", f.load("b")); !errors.Is(err, errEngineBusy) {
		t.Fatalf("background eviction: %v", err)
	}
	interactive()
	f.mu.Lock()
	f.models = nil
	f.mu.Unlock()
	if _, err := f.service.acquire(context.Background(), "b", "background", f.load("b")); !errors.Is(err, errEngineBusy) {
		t.Fatalf("grace: %v", err)
	}
	f.advance(15 * time.Minute)
	release, err := f.service.acquire(context.Background(), "b", "background", f.load("b"))
	if err != nil {
		t.Fatal(err)
	}
	release()
}
func TestEngineServiceMultipleResidents(t *testing.T) {
	f := newServiceFixture(2)
	a, err := f.service.acquire(context.Background(), "a", "interactive", f.load("a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.service.acquire(context.Background(), "b", "interactive", f.load("b"))
	if err != nil {
		t.Fatal(err)
	}
	c := requestService(t, f, "c")
	waitServiceQueue(t, f.service, 1)
	a()
	select {
	case <-c:
		t.Fatal("evicted the active b model")
	default:
	}
	b()
	serviceRelease(t, c)()
}
func TestEngineServiceIdleClock(t *testing.T) {
	f := newServiceFixture(1)
	release, err := f.service.acquire(context.Background(), "a", "interactive", f.load("a"))
	if err != nil {
		t.Fatal(err)
	}
	f.advance(time.Hour)
	f.service.idleTick()
	f.mu.Lock()
	if f.unloads != 0 {
		t.Error("idle interrupted inference")
	}
	f.mu.Unlock()
	release()
	f.advance(29 * time.Minute)
	f.service.idleTick()
	f.mu.Lock()
	if f.unloads != 0 {
		t.Error("early unload")
	}
	f.mu.Unlock()
	f.advance(time.Minute)
	f.service.idleTick()
	f.mu.Lock()
	if f.unloads != 1 {
		t.Error("idle did not unload")
	}
	f.mu.Unlock()
	release, err = f.service.acquire(context.Background(), "a", "interactive", f.load("a"))
	if err != nil {
		t.Fatal(err)
	}
	release()
	f.mu.Lock()
	f.policy.Idle = 0
	f.mu.Unlock()
	f.advance(time.Hour)
	f.service.idleTick()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unloads != 1 {
		t.Fatal("idle=0 unloaded")
	}
}

func TestEngineServiceConfiguredWait(t *testing.T) {
	f := newServiceFixture(1)
	f.policy.Wait = 1
	release, err := f.service.acquire(context.Background(), "a", "interactive", f.load("a"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	start := time.Now()
	if _, err := f.service.acquire(context.Background(), "b", "interactive", f.load("b")); !errors.Is(err, errEngineBusy) {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("wait was %s", elapsed)
	}
}
func TestEngineServiceNativeActivityLease(t *testing.T) {
	testHome(t)
	f := newServiceFixture(1)
	f.service.nativeBusy = engineNativeGenerating
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := engineGenerationLease(ctx)
	f.advance(time.Hour)
	f.service.idleTick()
	f.mu.Lock()
	if f.unloads != 0 {
		t.Error("unloaded during native tool pause")
	}
	f.mu.Unlock()
	end()
	f.service.idleTick()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unloads != 1 {
		t.Error("lease did not release")
	}
}

func TestEngineServiceMaintenanceDrainsAndBlocksOldArrivals(t *testing.T) {
	f := newServiceFixture(1)
	release, err := f.service.acquire(context.Background(), "a", "interactive", f.load("a"))
	if err != nil {
		t.Fatal(err)
	}
	action := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- f.service.maintain(context.Background(), "a", func() error { close(action); return nil })
	}()
	waitServiceQueue(t, f.service, 1)
	old := requestService(t, f, "a")
	waitServiceQueue(t, f.service, 2)
	select {
	case <-action:
		t.Fatal("capacity changed before drain")
	default:
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("maintenance hung")
	}
	serviceRelease(t, old)()
}
