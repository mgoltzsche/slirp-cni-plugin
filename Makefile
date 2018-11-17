SLIRP4NETNS_VERSION?=e5c7f16a3f866b2def1cc4d20587553342f6f6eb
CNI_VERSION?=0.6.0
BUILDTAGS?=linux static_build
LDFLAGS?=-extldflags '-static'
CGO_ENABLED?=0
GOOS?=linux

BUILDIMAGE=local/slirp-cni-plugin-build:latest
LITEIDEIMAGE=local/slirp-cni-plugin-build:liteide
DOCKER=docker
USER=$(shell [ '${DOCKER}' = docker ] && id -u || echo 0)
DOCKERRUN=${DOCKER} run --name slirp-cni-plugin-build --rm -e CGO_ENABLED=${CGO_ENABLED} -e GOOS=${GOOS} -e GOPATH=/work/build -v "${REPODIR}:/work" -w /work

REPODIR=$(shell pwd)
GOPATH=${REPODIR}/build
CNIGOPATH=${GOPATH}/cni
LITEIDE_WORKSPACE=${GOPATH}/liteide-workspace
PKGNAME=github.com/mgoltzsche/slirp-cni-plugin
PKGRELATIVEROOT=$(shell echo /src/${PKGNAME} | sed -E 's/\/+[^\/]*/..\//g')

all: slirp

slirp: .buildimage
	${DOCKERRUN} -u ${USER}:${USER} ${BUILDIMAGE} make .slirp

.slirp: vendor
	# Building slirp plugin:
	export GOPATH="${GOPATH}" && \
	go build -o slirp -a -ldflags "${LDFLAGS}" -tags "${BUILDTAGS}" "${PKGNAME}"

test: vendor
	# Run unit tests:
	export GOPATH="${GOPATH}" && \
	go test ${PKGNAME}

test-integration: cnitool slirp4netns
	# Run integration test:
	${DOCKERRUN} --privileged -u ${USER}:${USER} ${BUILDIMAGE} ./integration-test/test.sh

lint: .workspace
	export GOPATH="${GOPATH}"; \
	go get golang.org/x/lint/golint && \
	"${GOPATH}/bin/golint" ${PKGNAME}

fmt:
	# Format the go code
	(find . -mindepth 1 -maxdepth 1 -type d; ls *.go) | grep -Ev '^(./vendor|./build|./.git)(/.*)?$$' | xargs -n1 gofmt -w

validate:
	export GOPATH="${GOPATH}"; \
	go get github.com/vbatts/git-validation
	"${GOPATH}/bin/git-validation" -run DCO,short-subject

check: .buildimage
	${DOCKERRUN} -u ${USER}:${USER} ${BUILDIMAGE} make validate lint test
	make slirp test-integration
	# Test slirp binary
	./slirp || ([ $$? -eq 1 ] && echo slirp binary exists and is runnable)

slirp4netns:
	docker image build --force-rm -f Dockerfile --build-arg SLIRP4NETNS_VERSION=${SLIRP4NETNS_VERSION} --target slirp4netns --tag local/slirp4netns .
	id=$$(docker create local/slirp4netns) && \
	docker cp $$id:/slirp4netns/slirp4netns build/bin/slirp4netns && \
	docker rm -v $$id

cnitool: vendor
	docker image build --force-rm -f Dockerfile --build-arg SLIRP4NETNS_VERSION=${SLIRP4NETNS_VERSION} --target cnitool --tag local/cnitool .
	id=$$(docker create local/cnitool) && \
	docker cp $$id:/cnitool build/bin/cnitool && \
	docker rm -v $$id

.buildimage:
	# Building build image:
	${DOCKER} build --force-rm -f Dockerfile --target build-base -t ${BUILDIMAGE} .

build-sh: .buildimage
	# Running dockerized interactive build shell
	${DOCKERRUN} -ti ${BUILDIMAGE} /bin/sh

vendor: .workspace
ifeq ($(shell [ ! -d vendor -o "${UPDATE_VENDOR}" = TRUE ] && echo 0),0)
	# Fetching dependencies:
	GOPATH="${GOPATH}" go get github.com/LK4D4/vndr
	rm -rf "${GOPATH}/vndrtmp"
	mkdir "${GOPATH}/vndrtmp"
	ln -sf "${REPODIR}/vendor.conf" "${GOPATH}/vndrtmp/vendor.conf"
	(cd build/vndrtmp && "${GOPATH}/bin/vndr" -whitelist='.*')
	rm -rf vendor
	mv "${GOPATH}/vndrtmp/vendor" vendor
else
	# Skipping vendor update
endif

vendor-update:
	# Update vendor directory
	@make vendor UPDATE_VENDOR=TRUE
	# In case LiteIDE is running it must be restarted to apply the changes

.workspace:
	# Preparing build directory:
	[ -d "${GOPATH}/src/${PKGNAME}" ] || \
		(mkdir -p vendor "$(shell dirname "${GOPATH}/src/${PKGNAME}")" \
		&& ln -sf "${PKGRELATIVEROOT}" "${GOPATH}/src/${PKGNAME}")

liteide: vendor
	rm -rf "${LITEIDE_WORKSPACE}"
	mkdir "${LITEIDE_WORKSPACE}"
	cp -r vendor "${LITEIDE_WORKSPACE}/src"
	mkdir -p "${LITEIDE_WORKSPACE}/src/${PKGNAME}"
	ln -sr "${REPODIR}"/* "${LITEIDE_WORKSPACE}/src/${PKGNAME}"
	(cd "${LITEIDE_WORKSPACE}/src/${PKGNAME}" && rm build vendor dist)
	GOPATH="${LITEIDE_WORKSPACE}" \
	BUILDFLAGS="-tags \"${BUILDTAGS}\"" \
	liteide "${LITEIDE_WORKSPACE}/src/${PKGNAME}" &
	################################################################
	# Setup LiteIDE project using the main package's context menu: #
	#  - 'Build Path Configuration':                               #
	#    - Make sure 'Inherit System GOPATH' is checked!           #
	#    - Configure BUILDFLAGS variable printed above             #
	#  - 'Lock Build Path' to the top-level directory              #
	#                                                              #
	# CREATE NEW TOP LEVEL PACKAGES IN THE REPOSITORY DIRECTORY    #
	# EXTERNALLY AND RESTART LiteIDE WITH THIS COMMAND!            #
	################################################################

ide: .liteideimage
	# Make sure to lock the build path to the top-level directory
	ctnr bundle create -b slirp-liteide --update=true -w /work \
		--mount "src=${REPODIR},dst=/work/src/github.com/mgoltzsche/slirp-cni-plugin" \
		--mount src=/etc/machine-id,dst=/etc/machine-id,opt=ro \
		--mount src=/tmp/.X11-unix,dst=/tmp/.X11-unix \
		--env DISPLAY=$$DISPLAY \
		--env GOPATH=/work \
		${LITEIDEIMAGE} \
		liteide /work/src/github.com/mgoltzsche/slirp-cni-plugin
	ctnr bundle run --verbose slirp-liteide &

.liteideimage:
	ctnr image build --dockerfile=Dockerfile --target=liteide --tag=${LITEIDEIMAGE}

clean:
	rm -rf ./build ./slirp ./slirp-cni-plugin
