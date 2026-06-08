FROM mcr.microsoft.com/devcontainers/base:2.0.4-noble

RUN apt-get update && export DEBIAN_FRONTEND=noninteractive \
    && echo "wireshark-common wireshark-common/install-setuid boolean false" | debconf-set-selections \
    && apt-get -y install --no-install-recommends \
        termshark \
        tcpdump \
        tshark \
        wireshark-common \
    && rm -rf /var/lib/apt/lists/*
