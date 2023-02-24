# Mir Scripts

`validator.sh` runs Mir validator.

`daemon.sh` - runs Lotus daemon (node).

`4-node-net.sh` runs Mir the root subnet consisting of 4 validators and 4 nodes.

`update-valset.sh` updates the validator set of the 4 node subnet.

## Scenario 1
This scenario is used to demonstrate reconfiguration of the subnet.
The root subnet is run with four validators and then a new validator (`validator 4`) joins the subnet.

Run the subnet:
```shell
cd ./scripts/mir
./4-node-net.sh
```
This command runs the subnet with the default configuration defined in `./configs/valset_0.json`.

Get the network address of the `node 0` of the validators:
```shell
LOTUS_PATH=~/.lotus-local-net0 ./eudico net listen
```

Run `node 4`:
```shell
./daemon.sh 4
```
Run `validator 4`:
```shell
./validator.sh 4
```
This command runs the subnet with the default configuration defined in `./configs/valset_1.json`.

Then connect `node 4` to `node 0` using the network address we have got:
```shell
LOTUS_PATH=~/.lotus-local-net4 ./eudico net connect $NODE0-NET-ADDR
```

Update configuration:
```shell
./update-valset.sh 1
```
This command copies the configuration defined in `./configs/valset_1.json`
containing `validator 4` into validator folders.

In the terminal where `validator 4` is running you should see that the validator is syncing
and then mines new blocks.

Stop the network:
```shell
tmux kill-session -t mir
```