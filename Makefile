VERSION := $(shell cat VERSION)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test vet fmt clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o chistore .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -f chistore chistore.exe
