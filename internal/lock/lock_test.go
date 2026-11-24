package lock

import (
	"errors"
	"testing"
	"time"
)

func TestTryAcquireExclusiveAndAllOrNothing(t *testing.T) {
	ws := t.TempDir()
	h1, err := TryAcquire(ws, []string{"core"}, "test one")
	if err != nil {
		t.Fatal(err)
	}
	// core held → acquiring {core, erp} must fail and leave erp free
	_, err = TryAcquire(ws, []string{"erp", "core"}, "test two")
	var be *BusyError
	if !errors.As(err, &be) || be.Repo != "core" || be.Owner == "" {
		t.Fatalf("want BusyError for core with owner, got %v", err)
	}
	if locked, _ := Holder(ws, "erp"); locked {
		t.Fatal("erp must not stay locked after a failed all-or-nothing acquire")
	}
	if locked, owner := Holder(ws, "core"); !locked || owner == "" {
		t.Fatalf("core should be held, owner %q", owner)
	}
	h1.Release()
	if locked, _ := Holder(ws, "core"); locked {
		t.Fatal("core should be free after release")
	}
}

func TestAcquireWaits(t *testing.T) {
	ws := t.TempDir()
	h1, _ := TryAcquire(ws, []string{"core"}, "holder")
	go func() { time.Sleep(400 * time.Millisecond); h1.Release() }()
	waited := false
	h2, err := Acquire(ws, []string{"core"}, "waiter", 5*time.Second, func(*BusyError) { waited = true })
	if err != nil || !waited {
		t.Fatalf("Acquire: err=%v waited=%v", err, waited)
	}
	h2.Release()
	h3, _ := TryAcquire(ws, []string{"core"}, "holder")
	if _, err := Acquire(ws, []string{"core"}, "x", 0, nil); err == nil {
		t.Fatal("timeout 0 must fail immediately when busy")
	}
	h3.Release()
}
