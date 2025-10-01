FROM golang AS build

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY zooid zooid
COPY cmd cmd

RUN CGO_ENABLED=1 GOOS=linux go build -o bin/zooid cmd/relay/main.go

FROM gcr.io/distroless/base-debian12 AS run

WORKDIR /

COPY --from=build /app/bin/zooid /bin/zooid

USER nonroot:nonroot

EXPOSE 3334

ENV CONFIG=/tmp/config
ENV MEDIA=/tmp/media
ENV DATA=/tmp/data

ENTRYPOINT ["/bin/zooid"]
