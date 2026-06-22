# Multi-stage build for the cofiswarm-observer-gateway (WebSocket <-> ZMQ bus bridge).
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/app ./cmd/cofiswarm-observer-gateway

FROM alpine:3.20
RUN adduser -D -u 10001 app
COPY --from=build /out/app /usr/local/bin/cofiswarm-observer-gateway
USER app
EXPOSE 8820
ENTRYPOINT ["/usr/local/bin/cofiswarm-observer-gateway"]
