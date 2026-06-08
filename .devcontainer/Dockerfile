FROM mcr.microsoft.com/devcontainers/base:2.0.4-noble

ARG WEBSOCAT_VERSION=1.14.0

RUN apt-get update && export DEBIAN_FRONTEND=noninteractive \
    && echo "wireshark-common wireshark-common/install-setuid boolean false" | debconf-set-selections \
    && apt-get -y install --no-install-recommends \
        ca-certificates \
        curl \
        golang-go \
        nodejs \
        npm \
        termshark \
        tcpdump \
        tshark \
        wireshark-common \
    && npm install -g wscat@6.1.0 \
    && npm cache clean --force \
    && curl -fsSL "https://github.com/vi/websocat/releases/download/v${WEBSOCAT_VERSION}/websocat.x86_64-unknown-linux-musl" \
        -o /usr/local/bin/websocat \
    && chmod +x /usr/local/bin/websocat \
    && rm -rf /var/lib/apt/lists/*
