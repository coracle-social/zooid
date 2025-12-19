run:
  go run cmd/relay/main.go

run-debug:
  DEBUG=true just run

build:
  CGO_ENABLED=1 go build -o bin/zooid cmd/relay/main.go

test:
  go test -count=1 -v ./...

fmt:
  gofmt -w -s .
