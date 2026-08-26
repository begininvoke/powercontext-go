// Package handoffreport implements the optional Project catalog, independent
// Activity journal, Workspace bindings, and deterministic Handoff reports.
//
// The package is deliberately free of SQL, HTTP, and process lifecycle.  Its
// values are validated immutable snapshots; runtime and storage adapters own
// orchestration and persistence.
package handoffreport
