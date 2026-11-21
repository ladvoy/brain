package api

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.9.0 -generate types,chi-server,spec -package gen -o gen/api.gen.go ../../specs/001-brain-core/contracts/openapi.yaml
