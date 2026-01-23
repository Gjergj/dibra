FROM ubuntu:22.04

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
