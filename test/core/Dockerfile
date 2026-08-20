FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive
ENV container=docker

# This host intentionally contains no Docker CLI, Engine, Compose, or buildx.
# Keep this package list limited to dependencies used by the core integration
# families.
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    bzip2 \
    ca-certificates \
    cron \
    curl \
    git \
    gnupg \
    iproute2 \
    iptables \
    openssh-server \
    openssl \
    procps \
    python3 \
    rsyslog \
    sudo \
    systemd \
    systemd-sysv \
    tar \
    unzip \
    xz-utils \
    zip \
    && rm -rf /var/lib/apt/lists/*

RUN cd /lib/systemd/system/sysinit.target.wants/ && \
    ls | grep -v systemd-tmpfiles-setup | xargs rm -f && \
    rm -f /lib/systemd/system/multi-user.target.wants/* && \
    rm -f /etc/systemd/system/*.wants/* && \
    rm -f /lib/systemd/system/local-fs.target.wants/* && \
    rm -f /lib/systemd/system/sockets.target.wants/*udev* && \
    rm -f /lib/systemd/system/sockets.target.wants/*initctl* && \
    rm -f /lib/systemd/system/basic.target.wants/* && \
    rm -f /lib/systemd/system/anaconda.target.wants/*

RUN useradd -m -s /bin/bash testuser && \
    echo "testuser:testpass" | chpasswd && \
    echo "root:rootpass" | chpasswd && \
    usermod -aG sudo testuser && \
    printf '%s\n' "testuser ALL=(ALL) ALL" > /etc/sudoers.d/testuser && \
    chmod 0440 /etc/sudoers.d/testuser

RUN mkdir -p /etc/ssh/sshd_config.d /run/sshd && \
    printf '%s\n' \
      "PermitRootLogin yes" \
      "PasswordAuthentication yes" \
      "UsePAM yes" \
      > /etc/ssh/sshd_config.d/99-dibra-integration.conf

RUN systemctl enable ssh && \
    ln -sf /lib/systemd/system/systemd-user-sessions.service \
      /etc/systemd/system/multi-user.target.wants/systemd-user-sessions.service && \
    mkdir -p /etc/apt/keyrings

VOLUME ["/sys/fs/cgroup"]

EXPOSE 22

STOPSIGNAL SIGRTMIN+3

CMD ["/lib/systemd/systemd"]
