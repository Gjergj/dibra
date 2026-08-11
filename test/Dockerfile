FROM ubuntu:22.04

ARG COMPOSE_VERSION=5.4.0

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
    docker.io \
    && rm -rf /var/lib/apt/lists/*

# Install Docker Plugins manually
RUN mkdir -p /usr/local/lib/docker/cli-plugins && \
    curl -fSL https://github.com/docker/compose/releases/download/v${COMPOSE_VERSION}/docker-compose-linux-$(uname -m) -o /usr/local/lib/docker/cli-plugins/docker-compose && \
    curl -SL https://github.com/docker/buildx/releases/download/v0.13.1/buildx-v0.13.1.linux-$(uname -m | sed s/x86_64/amd64/ | sed s/aarch64/arm64/) -o /usr/local/lib/docker/cli-plugins/docker-buildx && \
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

RUN systemctl enable ssh

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

VOLUME ["/sys/fs/cgroup", "/tmp", "/run"]

EXPOSE 22

STOPSIGNAL SIGRTMIN+3

CMD ["/lib/systemd/systemd"]
