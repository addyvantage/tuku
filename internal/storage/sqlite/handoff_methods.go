package sqlite

import "tuku/internal/storage"

func (s *Store) Handoffs() storage.HandoffStore {
	return &handoffRepo{q: s.db}
}

func (s *txStore) Handoffs() storage.HandoffStore {
	return &handoffRepo{q: s.tx}
}
