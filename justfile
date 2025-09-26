run:
  go run cmd/relay/main.go

build:
  go build -o bin/zooid cmd/relay/main.go

test:
  go test -v ./...

fmt:
  gofmt -w -s .
