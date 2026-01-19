FROM golang:1.22 AS build

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY zooid zooid
COPY cmd cmd

RUN CGO_ENABLED=1 GOOS=linux go build -o bin/zooid cmd/relay/main.go

FROM debian:bookworm-slim AS run

# Install ca-certificates for HTTPS and create non-root user
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    useradd -r -u 65534 -g nogroup nonroot

WORKDIR /app

COPY --from=build /app/bin/zooid /bin/zooid
COPY templates /templates
COPY static /static
COPY docker-entrypoint.sh /docker-entrypoint.sh

RUN chmod +x /docker-entrypoint.sh && \
    mkdir -p /app/config /app/data /app/media && \
    chown -R nonroot:nogroup /app

USER nonroot

EXPOSE 3334

ENV PORT=3334
ENV CONFIG=/app/config
ENV MEDIA=/app/media
ENV DATA=/app/data

ENTRYPOINT ["/docker-entrypoint.sh"]
