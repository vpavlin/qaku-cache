REPO = quay.io/vpavlin0/codex-qaku-cache
TAG = $(shell git rev-parse --short HEAD)-$(shell git diff | base64 | sha256sum | cut -c 1-6)
IMAGE = $(REPO):$(TAG)

LIBWAKU_DEP_PATH=$(shell go list -m -f '{{.Dir}}' github.com/waku-org/waku-go-bindings)
LIBWAKU_PATH=$(LIBWAKU_DEP_PATH)/third_party/nwaku/build
CGO_LDFLAGS="-L$(LIBWAKU_PATH) -Wl,-rpath -Wl,$(LIBWAKU_PATH)"
CGO_CFLAGS="-I$(LIBWAKU_PATH)"

all: build

buildlib:
	cd $(LIBWAKU_DEP_PATH) &&\
	sudo mkdir -p third_party &&\
	sudo chown $(USER) third_party &&\
	make -C waku

cache:
	LD_LIBRARY_PATH=$(LD_LIBRARY_PATH):$(LIBWAKU_PATH) go run ./main.go
cache-build:
	CGO_CFLAGS=$(CGO_CFLAGS) CGO_LDFLAGS=$(CGO_LDFLAGS) go build -o _build/cachenode
build: cache-build
	mkdir -p _libwaku
	cp -R $(LIBWAKU_PATH)/* _libwaku/
	docker build -t $(IMAGE) .
push: build
	docker push $(IMAGE)