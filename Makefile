IMAGE ?= ghcr.io/sidero-community/talos-vip-watcher
TAG ?= dev
PLATFORMS ?= linux/amd64,linux/arm64

.PHONY: build test test-netns lint image

build:
	CGO_ENABLED=0 go build -trimpath -o bin/talos-vip-watcher .

test:
	go test -race ./...

# The netlink tests create and delete interfaces, so they run in a throwaway network namespace
# (an unprivileged user namespace; use "sudo unshare -n" where those are disabled).
test-netns:
	go test -c -race -o bin/vip.test .
	unshare -rn env TALOS_VIP_WATCHER_NETNS=1 ./bin/vip.test -test.run Netns -test.v

lint:
	golangci-lint run ./...

image:
	docker buildx build --platform $(PLATFORMS) --build-arg VERSION=$(TAG) -t $(IMAGE):$(TAG) .
