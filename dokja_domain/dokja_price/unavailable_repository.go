package price

import (
	"context"
	"errors"
)

// UnavailableRepository pauses price operations when durable storage cannot be
// opened. Other assistant domains can still run; no write succeeds in memory.
type UnavailableRepository struct{}

var ErrPersistenceUnavailable = errors.New("price persistence unavailable")

func (UnavailableRepository) Upsert(context.Context, Observation) (int, error) {
	return 0, ErrPersistenceUnavailable
}
func (UnavailableRepository) History(context.Context, Key, string, string) ([]Point, error) {
	return nil, ErrPersistenceUnavailable
}
func (UnavailableRepository) Keys(context.Context, string) ([]Key, error) {
	return nil, ErrPersistenceUnavailable
}
func (UnavailableRepository) Products(context.Context) ([]Product, error) {
	return nil, ErrPersistenceUnavailable
}
func (UnavailableRepository) Merchants(context.Context) ([]string, error) {
	return nil, ErrPersistenceUnavailable
}
func (UnavailableRepository) OfferURLs(context.Context, string) (map[Key]string, error) {
	return nil, ErrPersistenceUnavailable
}
func (UnavailableRepository) Watches(context.Context) ([]Watch, error) {
	return nil, ErrPersistenceUnavailable
}
func (UnavailableRepository) AddWatch(context.Context, Watch) (bool, error) {
	return false, ErrPersistenceUnavailable
}
func (UnavailableRepository) RemoveWatch(context.Context, Watch) (bool, error) {
	return false, ErrPersistenceUnavailable
}
func (UnavailableRepository) SaveDecision(context.Context, DecisionMemory) error {
	return ErrPersistenceUnavailable
}
func (UnavailableRepository) RecentDecisions(context.Context, string, string, int) ([]DecisionMemory, error) {
	return nil, ErrPersistenceUnavailable
}
