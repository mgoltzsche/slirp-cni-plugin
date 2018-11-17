FROM golang:alpine3.8 AS build-base
RUN apk add --update --no-cache gcc musl-dev make git

FROM build-base AS liteide
ARG LITEIDE_PKGS="g++ qt5-qttools qt5-qtbase-dev qt5-qtbase-x11 qt5-qtwebkit xkeyboard-config libcanberra-gtk3 adwaita-icon-theme ttf-dejavu"
RUN apk add --update --no-cache ${LITEIDE_PKGS} || /usr/lib/qt5/bin/qmake -help >/dev/null
RUN git clone https://github.com/visualfc/liteide.git \
	&& cd liteide/build \
	&& ./update_pkg.sh && QTDIR=/usr/lib/qt5 ./build_linux.sh \
	&& rm -rf /usr/local/bin \
	&& ln -s `pwd`/liteide/bin /usr/local/bin

FROM alpine:3.8 AS slirp4netns
RUN apk add --update --no-cache build-base git autoconf automake linux-headers
ARG SLIRP4NETNS_VERSION
RUN git clone https://github.com/rootless-containers/slirp4netns.git \
	&& cd slirp4netns \
	&& git checkout $SLIRP4NETNS_VERSION
WORKDIR /slirp4netns
RUN ./autogen.sh \
	&& LDFLAGS=-static ./configure --prefix=/usr \
	&& make

FROM build-base AS cnitool
ADD vendor/github.com/containernetworking/cni /work/src/github.com/containernetworking/cni
ARG LDFLAGS="-extldflags '-static'"
ARG CGO_ENABLED=0
ARG GOOS=linux
ENV GOPATH /work
WORKDIR /work
RUN go build -o /cnitool -a -ldflags "${LDFLAGS}" github.com/containernetworking/cni/cnitool \
	&& rm -rf /root/.cache/*