
#!/usr/bin/env bash
set -e

if [ $# -ne 4 ]
then
    echo "Provide the port where lotus and validator will be listening as first and second arguments, the subnet id as third, and the path to the validator key as fourth"
    echo "Args: [port] [validator_libp2p_port] [subnet_id] [import_validator_key_absolute_path]"
    exit 1
fi

PORT=$1
VAL_PORT=$2
SUBNETID=${3%/}
VAL_KEY_ABSOLUTE_PATH=$4
CONTAINER_NAME=`echo ipc${SUBNETID}_${PORT} | sed 's/\//_/g'`

echo "[*] Running docker container for root in port $PORT"
img=`docker run -dit --add-host host.docker.internal:host-gateway -p $PORT:1234 -p $VAL_PORT:1347 -v $VAL_KEY_ABSOLUTE_PATH:/wallet.key:ro --name $CONTAINER_NAME --entrypoint "/scripts/ipc/entrypoints/eudico-subnet.sh" eudico $SUBNETID`
echo "[*] Waiting for the daemon to start"
docker exec -it $img  eudico wait-api --timeout 350s
sleep 10
name=`docker ps --format "{{.Names}}" --filter "id=$img"`
echo ">>> Subnet $SUBNETID daemon running in container: $img (friendly name: $name)"
token=`docker exec -it $img  eudico auth create-token --perm admin`
echo ">>> Token to $SUBNETID daemon: $token"
wallet=`docker exec -it $img  eudico wallet default`
echo ">>> Default wallet: $wallet"
echo ">>> Subnet subnet validator info:"
docker exec -it $img eudico mir validator config validator-addr | grep '127.0.0.1/tcp' | sed 's/^.*\/127.0.0.1/\/dns\/host.docker.internal/' | sed -E "s/\/[0-9]+\//\/$VAL_PORT\//"
echo ">>> API listening in host port $PORT"
echo ">>> Validator listening in host port $VAL_PORT"
