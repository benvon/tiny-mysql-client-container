# Tiny MySQL Client

A minimal, statically-linked MySQL client written in Go. This project aims to provide a small, portable MySQL client that can be used in containerized environments without requiring additional dependencies.

## Features

- Statically linked - no external dependencies
- Small binary size
- Basic MySQL client functionality
- Compatible with MySQL 5.7+, MariaDB 10.5+

## Building

```bash
go build -o mysql -ldflags="-s -w" ./cmd/mysql
```

## Usage

```bash
./mysql -h host -P port -u user -p password -D database
```

## License

Apache 2.0
