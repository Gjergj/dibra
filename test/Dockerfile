FROM ubuntu:22.04

ARG DOCKER_ENGINE_VERSION=29.7.2
ARG COMPOSE_VERSION=5.4.0
ARG BUILDX_VERSION=0.30.0

ENV DEBIAN_FRONTEND=noninteractive
ENV container=docker

RUN apt-get update && apt-get install -y \
    systemd \
    systemd-sysv \
    openssh-server \
    sudo \
    gnupg \
    curl \
    ca-certificates \
    cron \
    rsyslog \
    ufw \
    iptables \
    git \
    zip \
    unzip \
    bzip2 \
    xz-utils \
    cowsay \
    nginx \
    && rm -rf /var/lib/apt/lists/*

# The integration host runs its own daemon. This avoids silently testing
# against whichever Engine happens to back the developer's Docker Desktop.
RUN architecture="$(uname -m)" && \
    curl -fSL "https://download.docker.com/linux/static/stable/${architecture}/docker-${DOCKER_ENGINE_VERSION}.tgz" -o /tmp/docker.tgz && \
    tar -xzf /tmp/docker.tgz -C /usr/local/bin --strip-components=1 && \
    rm -f /tmp/docker.tgz

COPY docker.service /etc/systemd/system/docker.service

# Install Docker Plugins manually
RUN mkdir -p /usr/local/lib/docker/cli-plugins && \
    curl -fSL https://github.com/docker/compose/releases/download/v${COMPOSE_VERSION}/docker-compose-linux-$(uname -m) -o /usr/local/lib/docker/cli-plugins/docker-compose && \
    curl -SL https://github.com/docker/buildx/releases/download/v${BUILDX_VERSION}/buildx-v${BUILDX_VERSION}.linux-$(uname -m | sed s/x86_64/amd64/ | sed s/aarch64/arm64/) -o /usr/local/lib/docker/cli-plugins/docker-buildx && \
    chmod +x /usr/local/lib/docker/cli-plugins/docker-compose /usr/local/lib/docker/cli-plugins/docker-buildx

RUN cd /lib/systemd/system/sysinit.target.wants/ && \
    ls | grep -v systemd-tmpfiles-setup | xargs rm -f && \
    rm -f /lib/systemd/system/multi-user.target.wants/* && \
    rm -f /etc/systemd/system/*.wants/* && \
    rm -f /lib/systemd/system/local-fs.target.wants/* && \
    rm -f /lib/systemd/system/sockets.target.wants/*udev* && \
    rm -f /lib/systemd/system/sockets.target.wants/*initctl* && \
    rm -f /lib/systemd/system/basic.target.wants/* && \
    rm -f /lib/systemd/system/anaconda.target.wants/*

RUN systemctl enable ssh docker

RUN useradd -m -s /bin/bash testuser && \
    echo "testuser:testpass" | chpasswd && \
    echo "root:rootpass" | chpasswd && \
    usermod -aG sudo testuser && \
    echo "testuser ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers

RUN sed -i 's/#PermitRootLogin prohibit-password/PermitRootLogin yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PasswordAuthentication yes/PasswordAuthentication yes/' /etc/ssh/sshd_config

# Re-enable systemd-user-sessions to remove /run/nologin on boot
# This allows non-root SSH logins after boot
RUN ln -sf /lib/systemd/system/systemd-user-sessions.service /etc/systemd/system/multi-user.target.wants/

RUN mkdir -p /etc/apt/keyrings

VOLUME ["/sys/fs/cgroup", "/var/lib/docker", "/tmp", "/run"]

EXPOSE 22

STOPSIGNAL SIGRTMIN+3

CMD ["/lib/systemd/systemd"]
