#!/usr/bin/env bash

set -e

tmux new-session -d -s "mir" \; \
    new-window   -t "mir" \; \
    split-window -t "mir:0" -v \; \
    \
    send-keys -t "mir:0" "
        /scripts/ipc/src/subnet-daemon.sh /root" Enter \; \
    send-keys -t "mir:0.0" "
        /scripts/ipc/src/root-single-validator.sh" Enter \; \
    \
    attach-session -t "mir:0"
