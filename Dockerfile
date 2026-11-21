# syntax=docker/dockerfile:1
FROM golang:bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /brain ./cmd/brain

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /brain /brain
COPY migrations /migrations
COPY specs/001-brain-core/contracts/openapi.yaml /specs/001-brain-core/contracts/openapi.yaml
USER nonroot:nonroot
EXPOSE 8080
ENV BIND_ADDR=:8080
ENTRYPOINT ["/brain"]
