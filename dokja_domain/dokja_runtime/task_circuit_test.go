package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTaskCircuitPausesRepeatedFailuresAndProbesRecovery(t *testing.T) {
	var circuit taskCircuit
	now := time.Now()
	var old uint64
	for i := 0; i < 2; i++ {
		epoch, err := circuit.begin("codex", now)
		if err != nil {
			t.Fatal(err)
		}
		old = epoch
		circuit.finish("codex", epoch, &ChatProviderFailure{Code: ChatAuthentication}, now)
	}
	if _, err := circuit.begin("codex", now); err == nil {
		t.Fatal("provider was not paused")
	}
	circuit.finish("codex", old, nil, now)
	if retry, _ := circuit.status("codex", now); retry <= 0 {
		t.Fatal("old request cleared cooldown")
	}
	if _, err := circuit.begin("kimi", now); err != nil {
		t.Fatal("independent fallback blocked")
	}
	epoch, err := circuit.begin("codex", now.Add(taskCooldown+time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := circuit.begin("codex", now.Add(taskCooldown+time.Second)); err == nil {
		t.Fatal("concurrent recovery probe accepted")
	}
	circuit.finish("codex", epoch, nil, now.Add(taskCooldown+time.Second))
	if retry, _ := circuit.status("codex", now.Add(taskCooldown+time.Second)); retry != 0 {
		t.Fatal("successful recovery stayed paused")
	}
}

func TestTaskCircuitDoesNotPauseForCancellationOrTaskContractErrors(t *testing.T) {
	var circuit taskCircuit
	now := time.Now()
	for i := 0; i < 4; i++ {
		for _, failure := range []error{context.Canceled, context.DeadlineExceeded, errors.New("private validation detail")} {
			epoch, err := circuit.begin("kimi", now)
			if err != nil {
				t.Fatal(err)
			}
			circuit.finish("kimi", epoch, failure, now)
		}
	}
	if retry, _ := circuit.status("kimi", now); retry != 0 {
		t.Fatal("task-specific errors paused provider")
	}
}

func TestTaskSuccessResetsConsecutiveFailureCount(t *testing.T) {
	var circuit taskCircuit
	now := time.Now()
	for i := 0; i < 5; i++ {
		for _, failure := range []error{&ChatProviderFailure{Code: ChatUnavailable}, nil} {
			epoch, err := circuit.begin("kimi", now)
			if err != nil {
				t.Fatal("nonconsecutive failures paused provider", err)
			}
			circuit.finish("kimi", epoch, failure, now)
		}
	}
}
