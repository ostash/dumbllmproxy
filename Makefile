.DEFAULT_GOAL := all

GO      ?= go
NPM     ?= npm
BINARY  := dumbllmproxy
WEB_DIR := web
DIST    := $(WEB_DIR)/dist

WEB_SOURCES := $(shell find $(WEB_DIR)/src -type f 2>/dev/null) \
	$(WEB_DIR)/index.html \
	$(WEB_DIR)/package.json \
	$(WEB_DIR)/package-lock.json \
	$(WEB_DIR)/tsconfig.json \
	$(WEB_DIR)/vite.config.ts

GO_SOURCES := $(shell find . -name '*.go' -not -path './$(WEB_DIR)/node_modules/*')

.PHONY: all build frontend test clean distclean

all: build

build: $(BINARY)

frontend: $(DIST)/index.html

# Rebuild the bundle only when the sources or the installed dependencies change.
$(DIST)/index.html: $(WEB_SOURCES) $(WEB_DIR)/node_modules/.package-lock.json
	cd $(WEB_DIR) && $(NPM) run build

$(WEB_DIR)/node_modules/.package-lock.json: $(WEB_DIR)/package-lock.json
	cd $(WEB_DIR) && $(NPM) install --no-audit --no-fund
	@touch $@

$(BINARY): $(GO_SOURCES) $(DIST)/index.html
	$(GO) build -o $(BINARY) .

test: $(DIST)/index.html
	$(GO) test ./...

clean:
	rm -rf $(WEB_DIR)/node_modules $(BINARY)

distclean: clean
	rm -rf $(DIST)
