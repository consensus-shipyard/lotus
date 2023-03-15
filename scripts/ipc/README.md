# IPC infra scripts
This directory includes a set of convenient scripts to light the burden of running the infrastructure required to run a full IPC deployment when debugging and testing.

The scrips are organized as follows:
- The scripts on the root of this directories are all you need to deploy the basic infrastructure you need to start tinkering with IPC and the [IPC agent](https://github.com/consensus-shipyard/ipc-agent). These scripts run a set of docker containers that bundle the validators and daemons that you need to run minimal deployments of a rootnet and subnet nodes.
- `entrypoint` includes all entrypoint scripts used by the docker images to run rootnet and subnet nodes.
- `src` includes smaller scripts with simple deployment stages that are used by the entrypoints and the root scripts to orchestrate the deployments.

If you are a power user looking to debug or extend the basic IPC deployments feel free to dig into `src/` or open an issue with your request.

## Usage
All these scripts assume the existence that the docker image for `eudico` exists in your local environment. If this is not the case you can build it yourself going to the root path of this repo (where the [Dockerfile](../../Dockerfile) lives).

```bash
docker build -t eudico .
```

### Minimal rootnet deployment
In order to run IPC and test the deployment of subnets, we first need to run a rootnet. Running a Mir-based rootnet with a single validator for IPC using Docker is as simple as running the following script:
```
./scripts/ipc/run-root-docker-1val.sh <API_PORT>
```
As an argument we need to pass the port where we want the lotus daemon for rootnet to be listening. The result of the scripts is the deployment of this single validator subnet and the api token and default wallet that we can use to interact with this network through the IPC agent: 
```
$ ./scripts/ipc/run-root-docker-1val.sh 1235
[*] Running docker container for root in port 1235
[*] Waiting for the daemon to start
>>> Token to /root daemon: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJyZWFkIiwid3JpdGUiLCJzaWduIiwiYWRtaW4iXX0._-2xzek2c3QzYqt5MWdQYrpRtK_Kqi8uEu3bvcm0i40
>>> Default wallet: t1cp4q4lqsdhob23ysywffg2tvbmar5cshia4rweq
```

### Run a subnet node
To run a subnet node in docker bundled with a daemon and a validator you can use: 
```
./scripts/ipc/run-subnet-docker.sh <PORT> <SUBNET> <VAL_KEY>
```
This command runs a new node for a subnet `<SUBNETID>` listening at `<PORT>` and using as default wallet the one with a private key as the one provided as input. Usually this will be the wallet address from the root that you used to join the subnet. To get the private key for an address you can run the following command against your root (or parent) node: 
```
eudico wallet export <ADDRESS>
```
The result of deploying the subnet node shoul output something like this:
```
$ ./scripts/ipc/run-subnet-docker.sh 1233 /root/t01002 \
  7b2254797065223a22736563703235366b31222c22507269766174654b6579223a22336c55434341635558505269304834314352365a616d67782b50447354565465716844626c376a446556343d227d

[*] Running docker container for root in port 1233
[*] Waiting for the daemon to start

>>> Token to /root/t01002 daemon:
eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJyZWFkIiwid3JpdGUiLCJzaWduIiwiYWRtaW4iXX0.LdeLeO2dSHu_vcoZiqqRlC_2EXgajC89z_mVbXRBuPc
>>> Default wallet: t1cp4q4lqsdhob23ysywffg2tvbmar5cshia4rweq
```
