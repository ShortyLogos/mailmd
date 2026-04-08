.PHONY: build run test clean

BINARY=mailmd

build:
	go build -o $(BINARY) ./cmd/mailmd

run: build
	./$(BINARY)

test:
	go test ./... -v

clean:
	rm -f $(BINARY)
