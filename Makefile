.PHONY: build install test clean fmt vet

PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

build:
	go build -o wxctl$(shell go env GOEXE) ./cmd/wxctl

install: build
	install -d $(DESTDIR)$(BINDIR)
	install -m 755 wxctl $(DESTDIR)$(BINDIR)/wxctl

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -f wxctl
