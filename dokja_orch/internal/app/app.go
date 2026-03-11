package app

import (
	"fmt"
	"read_books/internal/logger"
)

type Component interface {
	Name() string
	Start() error
	Stop() error
}

type App struct {
	components []Component
}

func NewApp(components ...Component) *App {
	return &App{
		components: components,
	}
}

func (a *App) Run(stop <-chan struct{}) error {
	if a == nil {
		return fmt.Errorf("app is nil")
	}

	started := make([]Component, 0, len(a.components))
	for _, component := range a.components {
		if component == nil {
			continue
		}

		logger.Info(fmt.Sprintf("Starting component: %s", component.Name()))
		if err := component.Start(); err != nil {
			a.stopComponents(started)
			return fmt.Errorf("start %s: %w", component.Name(), err)
		}
		started = append(started, component)
	}

	<-stop
	a.stopComponents(started)
	return nil
}

func (a *App) stopComponents(components []Component) {
	for i := len(components) - 1; i >= 0; i-- {
		component := components[i]
		if component == nil {
			continue
		}

		logger.Info(fmt.Sprintf("Stopping component: %s", component.Name()))
		if err := component.Stop(); err != nil {
			logger.Error(fmt.Sprintf("Failed to stop component %s", component.Name()), err)
		}
	}
}
