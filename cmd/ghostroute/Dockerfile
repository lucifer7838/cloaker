FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /ghostroute ./cmd/ghostroute

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /ghostroute /usr/local/bin/ghostroute
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/ghostroute"]
