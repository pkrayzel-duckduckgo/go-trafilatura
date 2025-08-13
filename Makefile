generate:
	@for name in internal/re2go/*.re; do \
		RE_IN=$$name; \
		RE_OUT=$$(echo $$name | sed 's/\.re/.go/'); \
		re2go -W -F --input-encoding utf8 --utf8 --no-generation-date -i $$RE_IN -o $$RE_OUT; \
		gofmt -w $$RE_OUT; \
	done

test: generate
	@echo "Test normal regex"
	@echo
	go test -timeout 30s ./...


# -------- defaults (override on command line) --------
IN ?=                     # required at runtime
OUT ?= metrics.csv
K ?= 5
# cross-platform default for CPU count
CONC ?= $(shell nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 8)
URL_FROM ?= canonical     # canonical|filename|none
EMIT_BODIES ?= false      # true to include body_a/body_b
LIMIT ?=                  # e.g. 1000

# turn booleans/optionals into CLI flags
ifeq ($(strip $(EMIT_BODIES)),true)
  EMIT_FLAG := --emit-bodies
endif
ifneq ($(strip $(LIMIT)),)
  LIMIT_FLAG := --limit $(LIMIT)
endif

build-gt:
	@go build -o go-trafilatura ./cmd/go-trafilatura

compare: build-gt
	@test -n "$(IN)" || { \
		echo "Usage: make compare IN=/path/to/html [OUT=metrics.csv K=5 CONC=$(CONC) URL_FROM=$(URL_FROM) EMIT_BODIES=false LIMIT=]"; \
		exit 2; \
	}
	@echo "Comparing control and variant of content extraction"
	@echo
	./go-trafilatura compare \
	  --in "$(IN)" \
	  --out "$(OUT)" \
	  --k $(K) \
	  --concurrency $(CONC) \
	  --url-from $(URL_FROM) \
	  $(EMIT_FLAG) $(LIMIT_FLAG)
