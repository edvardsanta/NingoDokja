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
	if missing := missingProviderValues(c.Values); len(missing) > 0 {
		// A row written before the registry gained a provider: the new keys take
		// their defaults, persisted when possible, like NewSettings does for the
		// operational catalog. A conflict only means another writer saved first;
		// the merged view still decodes.
		merged := make(map[string]string, len(c.Values)+len(missing))
		for key, value := range c.Values {
			merged[key] = value
		}
		for key, value := range missing {
			merged[key] = value
		}
		revision := c.Revision
		if updated, err := s.DB.UpdateProviderConfiguration(c.Revision, missing); err == nil {
			merged, revision = updated.Values, updated.Revision
		}
		return domain.DecodeProviderPreferences(revision, merged)
	}
	return domain.DecodeProviderPreferences(c.Revision, c.Values)
}

// missingProviderValues returns the default entries a stored row lacks for the
// current registry: registrations made before a provider joined the specs.
func missingProviderValues(values map[string]string) map[string]string {
	missing := map[string]string{}
	for key, value := range domain.DefaultProviderValues() {
		if _, ok := values[key]; !ok {
			missing[key] = value
		}
	}
	return missing
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
