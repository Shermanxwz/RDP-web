FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go test ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/rdpweb ./cmd/rdpweb

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -S -g 10001 rdpweb && adduser -S -D -H -u 10001 -G rdpweb rdpweb
WORKDIR /app
COPY --from=build /out/rdpweb /usr/local/bin/rdpweb
RUN mkdir -p /data && chown rdpweb:rdpweb /data
USER rdpweb
ENV RDPWEB_ADDR=:8080 RDPWEB_DATA_DIR=/data
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/rdpweb"]
