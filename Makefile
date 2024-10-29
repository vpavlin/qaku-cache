REPO = quay.io/vpavlin0/codex-qaku-cache
TAG = $(shell git rev-parse --short HEAD)-$(shell git diff | base64 | sha256sum | cut -c 1-6)
IMAGE = $(REPO):$(TAG)
cache:
	go run ./main.go
cache-build:
	go build -o _build/cachenode
build: cache-build
	docker build -t $(IMAGE) .
push: build
	docker push $(IMAGE)