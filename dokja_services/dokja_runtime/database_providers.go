package runtime

import (
	"errors"
	"fmt"

	domain "read_books/dokja_domain/dokja_runtime"
	store "read_books/dokja_store"
)

func (s *DatabaseSettings) providerSnapshot() (domain.ProviderPreferences, error) {
	c, exists, err := s.DB.ProviderConfiguration()
	if err != nil {
		return domain.ProviderPreferences{}, fmt.Errorf("cannot read provider configuration")
	}
	if !exists {
		c, err = s.DB.InitializeProviderConfiguration(domain.DefaultProviderValues())
		if err != nil {
			return domain.ProviderPreferences{}, fmt.Errorf("cannot initialize provider configuration")
		}
	}
	return domain.DecodeProviderPreferences(c.Revision, c.Values)
}
func (s *DatabaseSettings) ReadProviders() (domain.ProviderPreferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.providerSnapshot()
}
func (s *DatabaseSettings) WriteProvider(revision uint64, p domain.ProviderRegistration) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := domain.ProviderRegistrationValues(p)
	if err != nil {
		return 0, err
	}
	if _, err = s.providerSnapshot(); err != nil {
		return 0, err
	}
	c, err := s.DB.UpdateProviderConfiguration(revision, values)
	if errors.Is(err, store.ErrConfigurationConflict) {
		return 0, domain.ErrProviderConflict
	}
	if err != nil {
		return 0, fmt.Errorf("cannot save provider configuration")
	}
	return c.Revision, nil
}
