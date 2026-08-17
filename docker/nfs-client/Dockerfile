# Throwaway NFS client for smoke tests: the host may not ship mount.nfs, and
# a privileged container can drive the kernel NFS client just as well.
FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends nfs-common \
    && rm -rf /var/lib/apt/lists/*
