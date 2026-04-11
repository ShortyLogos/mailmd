.PHONY: build run test clean install

VERSION ?= dev
BINARY = mailmd
LDFLAGS = -s -w -X main.version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/mailmd
	codesign -s - $(BINARY)

run: build
	./$(BINARY)

test:
	go test ./... -v

install: build
	cp $(BINARY) ~/.local/bin/$(BINARY)

clean:
	rm -f $(BINARY)
