# syntax=docker/dockerfile:1

# lotus-builder-base ===========================================================
FROM golang:1.18.8-bullseye AS lotus-builder-base

LABEL org.opencontainers.image.authors="Lotus Development Team"

### taken from https://github.com/rust-lang/docker-rust/blob/2301a502c3ff8bbf30c32a6ef2114f3b363c4553/1.63.0/bullseye/Dockerfile
ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/cargo/bin:$PATH \
    RUST_VERSION=1.63.0

RUN set -eux; \
    dpkgArch="$(dpkg --print-architecture)"; \
    case "${dpkgArch##*-}" in \
        amd64) rustArch='x86_64-unknown-linux-gnu'; rustupSha256='5cc9ffd1026e82e7fb2eec2121ad71f4b0f044e88bca39207b3f6b769aaa799c' ;; \
        armhf) rustArch='armv7-unknown-linux-gnueabihf'; rustupSha256='48c5ecfd1409da93164af20cf4ac2c6f00688b15eb6ba65047f654060c844d85' ;; \
        arm64) rustArch='aarch64-unknown-linux-gnu'; rustupSha256='e189948e396d47254103a49c987e7fb0e5dd8e34b200aa4481ecc4b8e41fb929' ;; \
        i386) rustArch='i686-unknown-linux-gnu'; rustupSha256='0e0be29c560ad958ba52fcf06b3ea04435cb3cd674fbe11ce7d954093b9504fd' ;; \
        *) echo >&2 "unsupported architecture: ${dpkgArch}"; exit 1 ;; \
    esac; \
    url="https://static.rust-lang.org/rustup/archive/1.25.1/${rustArch}/rustup-init"; \
    wget "$url"; \
    echo "${rustupSha256} *rustup-init" | sha256sum -c -; \
    chmod +x rustup-init; \
    ./rustup-init -y --no-modify-path --profile minimal --default-toolchain $RUST_VERSION --default-host ${rustArch}; \
    rm rustup-init; \
    chmod -R a+w $RUSTUP_HOME $CARGO_HOME; \
    rustup --version; \
    cargo --version; \
    rustc --version;
### RUST


# lotus-builder ================================================================
FROM lotus-builder-base AS lotus-builder

LABEL org.opencontainers.image.authors="Lotus Development Team"

RUN <<EOF
    set -e

    apt-get update
    apt-get install -y      \
        jq                  \
        ca-certificates     \
        build-essential     \
        clang               \
        ocl-icd-opencl-dev  \
        ocl-icd-libopencl1  \
        libhwloc-dev

    rm -rf /var/lib/apt/lists/*
EOF

ENV XDG_CACHE_HOME="/tmp"

WORKDIR /opt/filecoin

# Download golang dependencies
COPY ./extern ./extern
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy project sources
COPY ./ ./

# RUN scripts/docker-git-state-check.sh

### make configurable filecoin-ffi build
ARG FFI_BUILD_FROM_SOURCE=${FFI_BUILD_FROM_SOURCE:-0}

RUN --mount=type=cache,target=$RUSTUP_HOME,from=lotus-builder-base,source=$RUSTUP_HOME \
    --mount=type=cache,target=$CARGO_HOME,from=lotus-builder-base,source=$CARGO_HOME \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make clean deps

ARG RUSTFLAGS=""
ARG GOFLAGS=""

RUN --mount=type=cache,target=$RUSTUP_HOME,from=lotus-builder-base,source=$RUSTUP_HOME \
    --mount=type=cache,target=$CARGO_HOME,from=lotus-builder-base,source=$CARGO_HOME \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make -j "$(grep -c processor /proc/cpuinfo)" buildall spacenet


# lotus-base ===================================================================
FROM debian:bullseye AS lotus-base

LABEL org.opencontainers.image.authors="Lotus Development Team"

RUN <<EOF
    set -x

    useradd -r -u 532 -U fc
    mkdir -p /etc/OpenCL/vendors
    echo "libnvidia-opencl.so.1" > /etc/OpenCL/vendors/nvidia.icd
EOF

# Base resources
COPY --from=lotus-builder  \
    /etc/ssl/certs         \
    /etc/ssl/certs

COPY --from=lotus-builder    \
    /lib/*/libdl.so          \
    /lib/*/librt.so          \
    /lib/*/libgcc_s.so       \
    /lib/*/libutil.so        \
    /usr/lib/*/libltdl.so    \
    /usr/lib/*/libnuma.so    \
    /usr/lib/*/libhwloc.so   \
    /usr/lib/*/libOpenCL.so  \
    /lib/


# lotus ========================================================================
FROM lotus-base AS lotus

LABEL org.opencontainers.image.authors="Lotus Development Team"

RUN <<EOF
    set -e

    mkdir -p \
        /var/lib/lotus \
        /var/tmp/filecoin-proof-parameters

    chown fc: \
        /var/lib/lotus \
        /var/tmp/filecoin-proof-parameters
EOF

COPY ./scripts/docker-eudico-entrypoint.sh /

COPY --from=lotus-builder     \
    /opt/filecoin/lotus       \
    /opt/filecoin/lotus-shed  \
    /usr/local/bin/

ARG DOCKER_LOTUS_IMPORT_SNAPSHOT https://snapshots.mainnet.filops.net/minimal/latest
ENV DOCKER_LOTUS_IMPORT_SNAPSHOT ${DOCKER_LOTUS_IMPORT_SNAPSHOT}
ENV FILECOIN_PARAMETER_CACHE /var/tmp/filecoin-proof-parameters
ENV LOTUS_PATH /var/lib/lotus
ENV DOCKER_LOTUS_IMPORT_WALLET ""

USER fc

VOLUME /var/lib/lotus
VOLUME /var/tmp/filecoin-proof-parameters

EXPOSE 1234

ENTRYPOINT [ "/docker-eudico-entrypoint.sh" ]
CMD [ "-help" ]


# lotus-all-in-one =============================================================
FROM lotus-base AS lotus-all-in-one

LABEL org.opencontainers.image.authors="Lotus Development Team"

# tmux to bundle daemon and validator
RUN <<EOF
    set -e

    apt-get update
    apt-get install -y tmux curl
EOF

ENV FILECOIN_PARAMETER_CACHE /var/tmp/filecoin-proof-parameters
ENV LOTUS_MINER_PATH /var/lib/lotus-miner
ENV LOTUS_PATH /var/lib/lotus
ENV LOTUS_WORKER_PATH /var/lib/lotus-worker
ENV WALLET_PATH /var/lib/lotus-wallet

# Copy subnets genesis template
COPY --from=lotus-builder /opt/filecoin/eudico-core/genesis/genesis.json /

COPY --from=lotus-builder         \
    /opt/filecoin/lotus           \
    /opt/filecoin/eudico          \
    /opt/filecoin/lotus-seed      \
    /opt/filecoin/lotus-shed      \
    /opt/filecoin/lotus-wallet    \
    /opt/filecoin/lotus-gateway   \
    /opt/filecoin/lotus-miner     \
    /opt/filecoin/lotus-worker    \
    /opt/filecoin/lotus-stats     \
    /opt/filecoin/lotus-fountain  \
    /usr/local/bin/

RUN <<EOF
    set -e

    mkdir -p \
        /var/tmp/filecoin-proof-parameters  \
        /var/lib/lotus                      \
        /var/lib/lotus-miner                \
        /var/lib/lotus-worker               \
        /var/lib/lotus-wallet

    chown fc: \
        /var/tmp/filecoin-proof-parameters  \
        /var/lib/lotus                      \
        /var/lib/lotus-miner                \
        /var/lib/lotus-worker               \
        /var/lib/lotus-wallet
EOF

VOLUME /var/tmp/filecoin-proof-parameters
VOLUME /var/lib/lotus
VOLUME /var/lib/lotus-miner
VOLUME /var/lib/lotus-worker
VOLUME /var/lib/lotus-wallet

EXPOSE 1234
EXPOSE 2345
EXPOSE 3456
EXPOSE 1777
