#!/usr/bin/env bash

INDEX=$1
PORT=$2
STORE=$3

tmux new-session -d -s "mir" \; \
  new-window   -t "mir"      \; \
  split-window -t "mir:0" -v \; \
  send-keys -t "mir:0.0" "./scripts/mir/daemon.sh $INDEX $PORT 2>&1 | tee $STORE/daemon_$INDEX.log"       Enter \; \
  send-keys -t "mir:0.1" "./scripts/mir/validator.sh $INDEX 2>&1 | tee $STORE/validator_$INDEX.log" Enter \;

tail -f /dev/null