.PHONY: build build-linux build-freebsd build-all clean run generate

BINARY=rsync-web

build:
	go build -o $(BINARY) ./cmd/srv

build-linux:
	GOOS=linux GOARCH=amd64 go build -o $(BINARY)-linux-amd64 ./cmd/srv

build-freebsd:
	GOOS=freebsd GOARCH=amd64 go build -o $(BINARY)-freebsd-amd64 ./cmd/srv

build-all: build-linux build-freebsd
	@echo "Built: $(BINARY)-linux-amd64, $(BINARY)-freebsd-amd64"

clean:
	rm -f $(BINARY) $(BINARY)-linux-amd64 $(BINARY)-freebsd-amd64

run: build
	./$(BINARY)

generate:
	go generate ./db/...
