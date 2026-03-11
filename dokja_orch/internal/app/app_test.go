package app

import (
	"errors"
	"testing"
)

type fakeComponent struct {
	name       string
	startErr   error
	startCount int
	stopCount  int
}

func (f *fakeComponent) Name() string {
	return f.name
}

func (f *fakeComponent) Start() error {
	f.startCount++
	return f.startErr
}

func (f *fakeComponent) Stop() error {
	f.stopCount++
	return nil
}

func TestAppRunStartsAndStopsComponents(t *testing.T) {
	component := &fakeComponent{name: "bot"}
	stop := make(chan struct{}, 1)
	stop <- struct{}{}

	app := NewApp(component)
	if err := app.Run(stop); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if component.startCount != 1 {
		t.Fatalf("expected 1 start call, got %d", component.startCount)
	}

	if component.stopCount != 1 {
		t.Fatalf("expected 1 stop call, got %d", component.stopCount)
	}
}

func TestAppRunStopsStartedComponentsOnFailure(t *testing.T) {
	first := &fakeComponent{name: "first"}
	second := &fakeComponent{name: "second", startErr: errors.New("boom")}

	app := NewApp(first, second)
	err := app.Run(make(chan struct{}))
	if err == nil {
		t.Fatal("expected start failure")
	}

	if first.stopCount != 1 {
		t.Fatalf("expected first component to be stopped, got %d", first.stopCount)
	}

	if second.stopCount != 0 {
		t.Fatalf("expected failing component not to be stopped, got %d", second.stopCount)
	}
}
