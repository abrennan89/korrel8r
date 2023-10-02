# Image name without version tag.
IMG?=quay.io/korrel8r/korrel8r
# Image version tag, a semantic version of the form: vX.Y.Z-extras
TAG?=$(shell git describe)
# Kustomize overlay to use for `make deploy`.
OVERLAY?=replace-image
# Use podman or docker, whichever is available.
IMGTOOL?=$(shell which podman || which docker)

VERSION_TXT=cmd/korrel8r/version.txt

## Local build and test

help:				## Help for make targets
	@echo
	@echo Make targets; echo
	@grep ':.*\s##' Makefile | sed 's/:.*##/:/' | column -s: -t

all: generate lint test	 	## Verify code changes: generate, lint, and test.

tools:	     			## Install tools used to generate code and documentation.
	go install github.com/go-swagger/go-swagger/cmd/swagger@latest
	go install github.com/swaggo/swag/cmd/swag@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

VERSION_TXT=cmd/korrel8r/version.txt

generate:  $(VERSION_TXT) pkg/api/docs	## Run code generation, pre-build.
	hack/copyright.sh
	go mod tidy

$(VERSION_TXT): force
	test "$(file < $@)" = "$(TAG)" || echo $(TAG) > $@

pkg/api/docs: $(shell find pkg/api pkg/korrel8r -name *.go)
	swag init -q -g $(dir $@)/api.go -o $@
	swag fmt $(dir $@)
	swagger -q generate markdown -f $@/swagger.json doc --output doc/rest-api.md

lint:				## Run the linter to find possible errors.
	golangci-lint run --fix

build: $(VERSION_TXT)				## Build the korrel8r binary.
	go build -tags netgo ./cmd/korrel8r

test:				## Run all tests, requires a cluster.
	TEST_NO_SKIP=1 go test -timeout=1m -race ./...

cover:				## Run tests and show code coverage in browser.
	go test -coverprofile=test.cov ./...
	go tool cover --html test.cov; sleep 2 # Sleep required to let browser start up.

run: $(VERSION_TXT)             ## Run from source using checked-in default configuration.
	go run ./cmd/korrel8r/ web -c etc/korrel8r/korrel8r.yaml

## Build and deploy an image

IMAGE=$(IMG):$(TAG)

image: $(VERSION_TXT)           ## Build and push a korrel8r image. Set IMG to a _public_ like IMG=quay.io/myquayaccount/korrel8r
	$(IMGTOOL) build --tag=$(IMAGE) .
	$(IMGTOOL) push -q $(IMAGE)
	@echo $(IMAGE)

image-name:			## Print the image name with tag.
	@echo $(IMAGE)

IMAGE_KUSTOMIZATION=config/overlays/replace-image/kustomization.yaml
$(IMAGE_KUSTOMIZATION): force	# Force because it depends on make variables, we can't tell if it's out of date.
	mkdir -p $(dir $@)
	hack/replace-image.sh REPLACE_ME $(IMG) $(TAG) > $@

WATCH=kubectl get events -A --watch-only& trap "kill %%" EXIT;

deploy: $(IMAGE_KUSTOMIZATION)	## Deploy to a cluster using customize.
	$(WATCH) kubectl apply -k config/overlays/$(OVERLAY)
	$(WATCH) kubectl wait -n korrel8r --for=condition=available deployment.apps/korrel8r
	which oc >/dev/null && oc delete --ignore-not-found route/korrel8r && oc expose -n korrel8r svc/korrel8r

route-url:			## URL of route to korrel8r on cluster (requires openshift for route)
	@oc get route/korrel8r -o template='http://{{.spec.host}}'; echo

## Create a release

NEWTAG_ERR=$(error NEWTAG=$(NEWTAG) must be of the form vX.Y.Z)
CHECK_NEWTAG=$(if $(shell echo "$(NEWTAG)" | grep -E "^v[0-9]+\.[0-9]+\.[0-9]+$$"),,$(NEWTAG_ERR))

release:	      ## Create a release tag and commit, push images. Set NEWTAG=vX.Y.Z
	$(CHECK_NEWTAG)
	$(MAKE) all	      # Make sure existing workspace is clean.
	$(if $(shell status --porcelain),$(error git repository is dirty, cannot make $@))
	$(MAKE) $(VERSION_TXT) TAG=$(NEWTAG)	   # Update version
	hack/changelog.sh $(NEWTAG) > CHANGELOG.md #  Update CHANGELOG.md
	git commit -a -m "Release $(NEWTAG)"	   # Commit the release
	git tag $(TAG) -a -m "Release $(TAG)"	   # Tag the release
	git push origin $(TAG)			   # Push the release
	$(MAKE) latest

latest:				## Push the current image and a "latest" alias
	$(MAKE) image
	$(IMGTOOL) push "$(IMAGE)" "$(IMG):latest"

.PHONY: force # Dummy target that is never satisfied
