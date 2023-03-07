#!/usr/bin/env bash

if [ -n "$DOCKER_LOTUS_IMPORT_SNAPSHOT" ]; then
	GATE="$LOTUS_PATH"/date_initialized
	# Don't init if already initialized.
	if [ ! -f "$GATE" ]; then
		echo importing minimal snapshot
		/usr/local/bin/eudico mir daemon --import-snapshot "$DOCKER_LOTUS_IMPORT_SNAPSHOT" --halt-after-import
		# Block future inits
		date > "$GATE"
	fi
fi

# import wallet, if provided
if [ -n "$DOCKER_LOTUS_IMPORT_WALLET" ]; then
	/usr/local/bin/lotus-shed keyinfo import "$DOCKER_LOTUS_IMPORT_WALLET"
fi

exec /usr/local/bin/eudico "$@"
