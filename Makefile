.PHONY: build run tidy clean install

BINARY := bin/zeta
CMD    := ./cmd/zeta

build:
	go build -o $(BINARY) $(CMD)

run:
	go run $(CMD)

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)

install: build
	install -m 755 $(BINARY) $(HOME)/.local/bin/zeta
