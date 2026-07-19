FROM golang:1.22 AS builder
WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
RUN GOOS=linux CGO_ENABLED=0 go build -installsuffix cgo -o app ./cmd/splitty

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/app /app/app
RUN mkdir -p /var/data
ENTRYPOINT ["/app/app"]