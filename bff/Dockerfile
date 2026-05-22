FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/axis-bff .

FROM alpine:3.22
RUN adduser -D -H axis
USER axis
COPY --from=build /out/axis-bff /axis-bff
EXPOSE 8080
ENTRYPOINT ["/axis-bff"]
