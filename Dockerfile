# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy only the necessary files
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build a static binary
RUN CGO_ENABLED=0 GOOS=linux go build -o mysql \
    -ldflags="-s -w" \
    ./cmd/mysql

# Final stage
FROM scratch

COPY --from=builder /app/mysql /bin/mysql

ENTRYPOINT ["/bin/mysql"]
