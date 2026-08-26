package powercontext

// PowerContext binds explicitly selected component groups without owning
// databases, goroutines, environment configuration, or process lifecycle.
type PowerContext[S, A, T any] struct {
	sources   S
	artifacts A
	triggers  T
}

func New[S, A, T any](sources S, artifacts A, triggers T) PowerContext[S, A, T] {
	return PowerContext[S, A, T]{sources: sources, artifacts: artifacts, triggers: triggers}
}

func (p PowerContext[S, A, T]) Sources() S   { return p.sources }
func (p PowerContext[S, A, T]) Artifacts() A { return p.artifacts }
func (p PowerContext[S, A, T]) Triggers() T  { return p.triggers }
