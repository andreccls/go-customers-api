# syntax=docker/dockerfile:1

# --- dev: toolbox used by the Makefile (tests, race detector, lint). Not shipped. ---
FROM golang:1.24-alpine AS dev
# gcc + musl-dev: the race detector needs cgo.
RUN apk add --no-cache gcc musl-dev \
 && go install honnef.co/go/tools/cmd/staticcheck@v0.6.1
WORKDIR /src

# --- build: static binary ---
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# --- runtime: distroless, non-root, no shell ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/api"]
