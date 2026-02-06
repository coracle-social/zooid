run:
  go run cmd/relay/main.go

build-relay:
  CGO_ENABLED=1 go build -o bin/zooid cmd/relay/main.go

build-import:
  CGO_ENABLED=1 go build -o bin/import cmd/import/main.go

build: build-relay build-import

test:
  go test -v ./...

fmt:
  gofmt -w -s .
