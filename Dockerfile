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

# Add build arguments for MySQL credentials
ARG MYSQL_ROOT_PASSWORD
ARG MYSQL_ROOT_USER=root

# Set environment variables for MySQL credentials
ENV MYSQL_ROOT_PASSWORD=$MYSQL_ROOT_PASSWORD
ENV MYSQL_ROOT_USER=$MYSQL_ROOT_USER

COPY --from=builder /app/mysql /bin/mysql

ENTRYPOINT ["/bin/mysql"]
