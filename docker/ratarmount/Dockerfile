# ratarmount-rs as a userspace NFSv3 server.
#
# Uses the signed release .deb so apt resolves runtime deps (libarchive, fuse3
# libs). A musl/Alpine source build is a future size optimization; this stays
# a lab-grade single-purpose image.
FROM ubuntu:24.04

ARG RATARMOUNT_VERSION=0.1.22

ADD https://github.com/hilather/ratarmount-rs/releases/download/v${RATARMOUNT_VERSION}/ratarmount_${RATARMOUNT_VERSION}_amd64.deb /tmp/ratarmount.deb

RUN apt-get update \
    && apt-get install -y --no-install-recommends /tmp/ratarmount.deb \
    && rm -rf /var/lib/apt/lists/* /tmp/ratarmount.deb \
    && ratarmount --version

# Unprivileged: userspace NFS needs no root, no FUSE device. SQLite indexes
# land under $HOME, which we point at tmpfs.
USER 65532:65532
ENV HOME=/tmp \
    XDG_CACHE_HOME=/tmp

EXPOSE 20490/tcp

ENTRYPOINT ["ratarmount"]
CMD ["--nfs", "--nfs-bind", "0.0.0.0:20490", "/archives/fixtures.tar.gz"]
