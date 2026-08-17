package source

import "context"

// Store persists resolved Sources for later catalog reads and lineage.
type Store interface {
	Add(context.Context, Value) (Value, error)
}

// Service composes Source acquisition, persistence, and read operations.
type Service struct {
	catalog *Catalog
	store   Store
}

func NewService(catalog *Catalog, store Store) (*Service, error) {
	if catalog == nil {
		return nil, &InvalidAdapterError{Field: "catalog", Detail: "must not be nil"}
	}
	if store == nil || isNil(store) {
		return nil, &InvalidAdapterError{Field: "store", Detail: "must not be nil"}
	}
	return &Service{catalog: catalog, store: store}, nil
}

func (s *Service) Resolve(ctx context.Context, input any) (Value, error) {
	return s.catalog.Resolve(ctx, input)
}

func (s *Service) Add(ctx context.Context, value Value) (Value, error) {
	if _, err := s.catalog.Ref(value); err != nil {
		return nil, err
	}
	stored, err := s.store.Add(ctx, value)
	if err != nil {
		return nil, err
	}
	if _, err := s.catalog.Ref(stored); err != nil {
		return nil, err
	}
	return stored, nil
}

func (s *Service) Get(ctx context.Context, value Value) (Value, error) {
	return s.catalog.Get(ctx, value)
}

func (s *Service) List(ctx context.Context) ([]Value, error) {
	return s.catalog.List(ctx)
}

func (s *Service) Read(ctx context.Context, value Value) (any, error) {
	return s.catalog.Read(ctx, value)
}

func (s *Service) Ref(value Value) (Ref, error) { return s.catalog.Ref(value) }
