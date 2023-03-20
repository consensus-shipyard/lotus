
#!/usr/bin/env bash
set -e

if [ $# -ne 1 ]
then
    echo "Provide the running container for the validator as an argument"
    echo "Args: [validator container]"
    exit 1
fi

IMG=$1

echo "[*] Starting to mine on validator"
docker exec -it $IMG eudico mir validator run --membership=onchain --nosync --ipcagent-url=http://host.docker.internal:3030/json_rpc
