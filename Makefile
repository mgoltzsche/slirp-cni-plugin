BUILDIMAGE=local/slirp-cni-plugin-build:latest
LITEIDEIMAGE=local/slirp-cni-plugin-build:liteide
DOCKER=docker
USER=$(shell [ '${DOCKER}' = docker ] && id -u || echo 0)
DOCKERRUN=${DOCKER} run --name slirp-cni-plugin-build --rm -v "${REPODIR}:/work" -w /work

REPODIR=$(shell pwd)
GOPATH=${REPODIR}/build
LITEIDE_WORKSPACE=${GOPATH}/liteide-workspace
PKGNAME=github.com/mgoltzsche/slirp-cni-plugin
PKGRELATIVEROOT=$(shell echo /src/${PKGNAME} | sed -E 's/\/+[^\/]*/..\//g')
VENDORLOCK=${REPODIR}/vendor/ready
BINARY=dist/cni-plugins/slirp

COMMIT_ID=$(shell git rev-parse HEAD)
COMMIT_TAG=$(shell git describe --exact-match ${COMMIT_ID} || echo -n "dev")
COMMIT_DATE=$(shell git show -s --format=%ci ${COMMIT_ID})

BUILDTAGS_STATIC=${BUILDTAGS} linux static_build
LDFLAGS+=-X main.commit=${COMMIT_ID} -X main.version=${COMMIT_TAG} -X 'main.date=${COMMIT_DATE}'
LDFLAGS_STATIC=${LDFLAGS} -extldflags '-static'

PACKAGES:=$(shell go list $(BUILDFLAGS) . | grep -v github.com/mgoltzsche/slirp-cni-plugin/vendor)
CNI_VERSION?=0.6.0
CNIGOPATH=${GOPATH}/cni

all: slirp-static

slirp-static: .buildimage
	${DOCKERRUN} -u ${USER}:${USER} ${BUILDIMAGE} make slirp BUILDTAGS="${BUILDTAGS_STATIC}" LDFLAGS="${LDFLAGS_STATIC}"

slirp: vendor
	# Building application:
	GOPATH="${GOPATH}" \
	go build -o ${BINARY} -a -ldflags "${LDFLAGS}" -tags "${BUILDTAGS}" "${PKGNAME}"

fmt:
	# Format the go code
	(find . -mindepth 1 -maxdepth 1 -type d; ls *.go) | grep -Ev '^(./vendor|./dist|./build|./.git)(/.*)?$$' | xargs -n1 gofmt -w

lint:
	export GOPATH="${GOPATH}"; \
	go get golang.org/x/lint/golint && \
	"${GOPATH}/bin/golint" $(shell export GOPATH="${GOPATH}"; cd "${GOPATH}/src/${PKGNAME}" && go list -tags "${BUILDTAGS_STATIC}" ./... 2>/dev/null | grep -Ev '/vendor/|^${PKGNAME}/build/')

cni-tool:
	ctnr image build --verbose --dockerfile Dockerfile --build-arg CNI_VERSION=v${CNI_VERSION} --target cni-tool --tag local/cni-tool
	ctnr bundle create -b "${GOPATH}/cni-tool-bundle" --update local/cni-tool
	mkdir -p "${REPODIR}/dist/bin"
	cp -f "${GOPATH}/cni-tool-bundle/rootfs/cni-tool" "${REPODIR}/dist/bin/cni-tool"

cni-plugins-static: .buildimage
	${DOCKERRUN} ${BUILDIMAGE} make cni-plugins LDFLAGS="${LDFLAGS_STATIC}"

cni-plugins:
	mkdir -p "${CNIGOPATH}"
	wget -O "${CNIGOPATH}/cni-${CNI_VERSION}.tar.gz" "https://github.com/containernetworking/cni/archive/v${CNI_VERSION}.tar.gz"
	wget -O "${CNIGOPATH}/cni-plugins-${CNI_VERSION}.tar.gz" "https://github.com/containernetworking/plugins/archive/v${CNI_VERSION}.tar.gz"
	tar -xzf "${CNIGOPATH}/cni-${CNI_VERSION}.tar.gz" -C "${CNIGOPATH}"
	tar -xzf "${CNIGOPATH}/cni-plugins-${CNI_VERSION}.tar.gz" -C "${CNIGOPATH}"
	rm -rf "${CNIGOPATH}/src/github.com/containernetworking"
	mkdir -p "${CNIGOPATH}/src/github.com/containernetworking"
	mv "${CNIGOPATH}/cni-${CNI_VERSION}"     "${CNIGOPATH}/src/github.com/containernetworking/cni"
	mv "${CNIGOPATH}/plugins-${CNI_VERSION}" "${CNIGOPATH}/src/github.com/containernetworking/plugins"
	export GOPATH="${CNIGOPATH}" && \
	for TYPE in main ipam meta; do \
		for CNIPLUGIN in `ls ${CNIGOPATH}/src/github.com/containernetworking/plugins/plugins/$$TYPE`; do \
			(set -x; go build -o dist/cni-plugins/$$CNIPLUGIN -a -ldflags "${LDFLAGS}" github.com/containernetworking/plugins/plugins/$$TYPE/$$CNIPLUGIN) || exit 1; \
		done \
	done

.buildimage:
	# Building build image:
	${DOCKER} build -f Dockerfile --target slirp-cni-plugin-build -t ${BUILDIMAGE} .

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
	# Skipping dependency update
endif

update-vendor:
	# Update vendor directory
	@make vendor UPDATE_VENDOR=TRUE
	# In case LiteIDE is running it must be restarted to apply the changes

.workspace:
	# Preparing build directory:
	[ -d "${GOPATH}" ] || \
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
	rm -rf ./build ./dist ./slirp
