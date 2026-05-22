FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /companion ./cmd/api

FROM alpine:3.23
RUN apk add --no-cache ca-certificates wget
COPY --from=builder /companion /usr/local/bin/companion
EXPOSE 8080
CMD ["companion"]
