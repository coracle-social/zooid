run:
  go run cmd/relay/main.go

build:
  go build -o bin/zooid cmd/relay/main.go

fmt:
  gofmt -w -s .
