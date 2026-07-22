.PHONY: build run tidy clean install

BINARY  := bin/zeta
CMD     := ./cmd/zeta
VERSION := 0.1.0
LDFLAGS := -ldflags "-X github.com/axispx/zeta/internal/version.Version=$(VERSION)"

build:
	go build $(LDFLAGS) -o $(BINARY) $(CMD)

run:
	go run $(LDFLAGS) $(CMD)

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)

install: build
	install -m 755 $(BINARY) $(HOME)/.local/bin/zeta
