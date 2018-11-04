FROM golang:alpine3.8 AS slirp-cni-plugin-build
RUN apk add --update --no-cache gcc musl-dev libseccomp-dev btrfs-progs-dev lvm2-dev make git

FROM slirp-cni-plugin-build AS liteide
ARG LITEIDE_PKGS="g++ qt5-qttools qt5-qtbase-dev qt5-qtbase-x11 qt5-qtwebkit xkeyboard-config libcanberra-gtk3 adwaita-icon-theme ttf-dejavu"
RUN apk add --update --no-cache ${LITEIDE_PKGS} || /usr/lib/qt5/bin/qmake -help >/dev/null
RUN git clone https://github.com/visualfc/liteide.git \
	&& cd liteide/build \
	&& ./update_pkg.sh && QTDIR=/usr/lib/qt5 ./build_linux.sh \
	&& rm -rf /usr/local/bin \
	&& ln -s `pwd`/liteide/bin /usr/local/bin

FROM golang:alpine3.8 AS cni-tool
RUN apk add --update --no-cache build-base git
ARG CNI_VERSION=v0.6.0
RUN git clone https://github.com/containernetworking/cni.git /work/src/github.com/containernetworking/cni \
	&& cd /work/src/github.com/containernetworking/cni \
	&& git checkout $CNI_VERSION
ARG LDFLAGS="-extldflags '-static'"
ARG CGO_ENABLED=0
ARG GOOS=linux
ENV GOPATH /work
WORKDIR /work
RUN go build -o /cni-tool -a -ldflags "${LDFLAGS}" github.com/containernetworking/cni/cnitool \
	&& rm -rf /root/.cache/*