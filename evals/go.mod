// Marker module: keeps eval fixtures and scenario overlays out of the
// ocx module, so `go build ./...` and `go test ./...` at the repository
// root skip them. Nothing here is built as part of this module.
module ocx-evals

go 1.26.1
