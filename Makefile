PLUGIN_ID ?= multi-account-orchestrator
VERSION ?= 0.1.0
BUILD_DIR ?= dist
GO ?= go

GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)

ifeq ($(GOOS),windows)
	EXT = dll
else ifeq ($(GOOS),darwin)
	EXT = dylib
else
	EXT = so
endif

OUTPUT = $(BUILD_DIR)/$(PLUGIN_ID).$(EXT)

.PHONY: all test build clean package checksums

all: test build

test:
	$(GO) test -v ./...

build:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 $(GO) build -trimpath -buildmode=c-shared -ldflags "-s -w" -o $(OUTPUT) .
	rm -f $(BUILD_DIR)/$(PLUGIN_ID).h

package: build
	mkdir -p $(BUILD_DIR)
	cd $(BUILD_DIR) && (python3 -c "import zipfile; z=zipfile.ZipFile('$(PLUGIN_ID)_$(VERSION)_$(GOOS)_$(GOARCH).zip', 'w', zipfile.ZIP_DEFLATED); z.write('$(PLUGIN_ID).$(EXT)'); z.close()" 2>/dev/null || python -c "import zipfile; z=zipfile.ZipFile('$(PLUGIN_ID)_$(VERSION)_$(GOOS)_$(GOARCH).zip', 'w', zipfile.ZIP_DEFLATED); z.write('$(PLUGIN_ID).$(EXT)'); z.close()")

checksums:
	cd $(BUILD_DIR) && shasum -a 256 *.zip > checksums.txt

clean:
	rm -rf $(BUILD_DIR)
