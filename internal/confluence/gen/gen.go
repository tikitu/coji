// Package gen holds Go types generated from the Confluence Cloud REST API v2
// OpenAPI spec. Only models are generated (no HTTP client); the hand-written
// client in the parent package builds requests using these types. Regenerate
// after updating spec/ with `go generate ./internal/confluence/...`.
package gen

//go:generate go tool oapi-codegen -config cfg.yaml ../../../spec/openapi-v2.v3.txt
