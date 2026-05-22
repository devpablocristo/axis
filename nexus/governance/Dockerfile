FROM golang:1.26-alpine AS builder

# git is required by `go mod download` to resolve modules hosted on private
# git remotes (e.g. github.com/devpablocristo/platform/*). Without this the smoke
# build fails with `exec: "git": executable file not found in $PATH`.
RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /governance ./cmd/api

FROM alpine:3.23
RUN apk add --no-cache ca-certificates
COPY --from=builder /governance /usr/local/bin/governance
EXPOSE 8080
CMD ["governance"]
