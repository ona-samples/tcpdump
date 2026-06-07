FROM mcr.microsoft.com/devcontainers/base:2.0.4-noble

RUN apt-get update && export DEBIAN_FRONTEND=noninteractive \
    && apt-get -y install --no-install-recommends tcpdump \
    && rm -rf /var/lib/apt/lists/*
