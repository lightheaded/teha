# teha build targets. POSIX sh only.

BINARY  ?= teha
VERSION ?= dev
IMAGE   ?= teha:$(VERSION)
DB      ?= ./teha.db
ADDR    ?= 127.0.0.1:8637

.POSIX:
.PHONY: build test run seed lint docker clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BINARY) ./cmd/teha

test:
	go test ./...
	node --test internal/webui/assets/parse.test.mjs

run:
	go run ./cmd/teha -dev -addr $(ADDR) -db $(DB)

seed:
	go run ./cmd/teha -seed -db $(DB)

lint:
	@out=`gofmt -l .`; if [ -n "$$out" ]; then echo "gofmt found unformatted files:"; echo "$$out"; exit 1; fi
	go vet ./...

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .
	@docker image ls $(IMAGE) --format "image size: {{.Size}}"

clean:
	rm -f $(BINARY) coverage.out coverage.html
