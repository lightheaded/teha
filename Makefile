# teha build targets. POSIX sh only.

# The SQLite driver is pure Go, so nothing here needs a C compiler. Exporting
# this for every target keeps `make test` working on a machine that has no gcc
# and no Xcode command line tools.
export CGO_ENABLED = 0

BINARY  ?= teha
VERSION ?= dev
IMAGE   ?= teha:$(VERSION)
DB      ?= ./teha.db
ADDR    ?= 127.0.0.1:8637

.POSIX:
.PHONY: build test run seed lint docker clean desktop desktop-check desktop-dev

build:
	go build -trimpath -ldflags "-s -w -X main.buildVersion=$(VERSION)" -o $(BINARY) ./cmd/teha

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

# The desktop shell. It needs Rust and the Tauri command line tool, which the
# server does not. Each target names what is missing, rather than failing deep
# inside cargo with a message about a linker.
DESKTOP = desktop/src-tauri
TAURI_CLI_VERSION = 2.11.4

GUARD_RUST = command -v cargo >/dev/null || { echo "cargo is missing. Install Rust from https://rustup.rs, then open a new shell."; exit 1; }
GUARD_CC = command -v cc >/dev/null || { echo "no C linker. On macOS run: xcode-select --install"; exit 1; }
GUARD_TAURI = cargo tauri --version >/dev/null 2>&1 || { echo "the Tauri command line tool is missing. Run: cargo install tauri-cli --version $(TAURI_CLI_VERSION) --locked"; exit 1; }

# The cheap one. This is what CI runs, and it needs no bundle and no icon.
desktop-check:
	node --test desktop/tools/contract.test.mjs
	@$(GUARD_RUST)
	@$(GUARD_CC)
	cd $(DESKTOP) && cargo check

# A window on the screen, with the shell rebuilt on every change.
desktop-dev:
	@$(GUARD_RUST)
	@$(GUARD_CC)
	@$(GUARD_TAURI)
	cd $(DESKTOP) && cargo tauri dev

# The .app and the .dmg. Unsigned: signing is a Phase 4 decision, because the
# certificate carries a legal name. See docs/PLAN.md section 4.
desktop:
	@$(GUARD_RUST)
	@$(GUARD_CC)
	@$(GUARD_TAURI)
	cd $(DESKTOP) && cargo tauri build
	@echo "the bundle is in $(DESKTOP)/target/release/bundle"

clean:
	rm -f $(BINARY) coverage.out coverage.html
