.PHONY: all web server dev clean

all: web server

web:
	pnpm --dir web install
	pnpm --dir web build

server:
	go build -o bin/xissue ./cmd/xissue

dev:
	go run ./cmd/xissue

clean:
	rm -rf bin internal/server/dist
