FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/relayforge ./cmd/relayforge

FROM alpine:3.23

RUN apk add --no-cache ca-certificates && addgroup -S -g 10001 relayforge && adduser -S -D -H -u 10001 -G relayforge relayforge

COPY --from=build /out/relayforge /usr/local/bin/relayforge

USER relayforge
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD wget -q -T 2 -O /dev/null http://127.0.0.1:8080/health/ready || exit 1

ENTRYPOINT ["/usr/local/bin/relayforge"]
