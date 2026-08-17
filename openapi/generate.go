// Package openapi owns generation of the immutable HTTP contract.
package openapi

//go:generate go run ../tools/api-generate -spec powercontext.yaml -target ../api/v1 -package v1 -client-invoker ../client/invoker_gen.go
