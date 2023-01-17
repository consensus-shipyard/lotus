# Mir Validator
`mir-validator` is an application to manage a Mir validator.

## Running a validator.
Use `make mir-validator` or `make spacenet` to compile the application. To
run a validator you need to:
- First spawn a lotus-daemon.
- Initialize the configuration of mir-validator in $LOTUS_PATH. Be sure to have your $LOTUS_PATH set to the right directory, or it will use the default repo directory (`~/.lotus`).
`./eudico mir validator config init`
- The information about the validators membership for the network is currently stored
in `$LOTUS_PATH/mir.validators`. To add new validators to the membership list you can either
paste the information of the new validator in that file (with the format `<wallet_addr>@<multiaddr_with_/p2p_info>`), or run `./eudico mir validator config add-validator`.
- Run the validator process using `./eudico mir validator run --nosync`

## Persisting Mir checkpoints
Mir relies on the use of checkpoints to recover validators after a failure, and to allow full-nodes (or as we like to call them, _learners_) to verify the validity of blocks received through the network. Learners are not participating from the block validation process, and checkpoints are the only way for them to verify that the blocks received.

If you want to persist every checkpoint produced by mir in a specific path of your validator, you can set the enviroment variable `$CHECKPOINTS_REPO` to point to the directory where you want to store the checkpoints.

Alternatively, if by any chance you are not persisting these checkpoints in `$CHECKPOINTS_REPO` and you want to access them at some point, checkpoints are also persisted in the validator's database. To import and export a checkpoint from and to your validator's database you can use the following commands: 
```
# Import a checkpoint from a file
./eudico mir validator checkpoint --file <checkpoint_file>

# Export latst checkpoint to file from database
./eudico mir validator checkpoint export --output <output-file>

# Export checkpoint for specific height
./eudico mir validator checkpoint export --height <height>
```

> NOTE: `./eudico mir validator checkpoint` can't be used when the validator is running. These commands insantiate the validator database to access the checkpoints, so they can only be used when the validator is stopped. They are provided to be used when a validator fails (or is shut down) and checkpoints from the validator need to be recovered. If in the future there is a lot of demand for these commands to be available while the validator is running we'll add this feature (so let us know :) ).

## Restarting a validator
Validators can be restarted seamlessly by running again `./eudico mir validator run --nosync`. The validator itself will request the latest checkpoint to other validators and sync its state until it catches up and is able to participate from the consensus again. Alternatively, we may restart a validator from a specific checkpoint by providing the `--init-height` and `--init-checkpoint` flags to initialize the validator from a checkpoint at a specific hegiht, or from a checkpoint file, respectively.

For a validator to be able to recover its state successfully, it needs to have its Lotus daemon running, and its daemon needs to be connected to at least some other Lotus daemon in the network with the latest state (ideally another synced validator). The validator will fail to start if it doesn't find any good peer to sync from.

## Recovering a validator from a crash. 
Crashing validators can be restarted easily by just restarting them as shown in the previous section. However, there may be situations where: `f+1` validators fail and the network stalls, some state is corrupted and the network needs to be restarted from an older state, or there is an attack and we want to do the DAO bail-out again. In these cases, the network can be recovered from an older state as long as we have a valid Mir checkpoint for the desired height we want to recover from, and the corresponding Lotus state for that checkpoint. Ideally, a copy of the backed up Lotus state should be persisted as a snapshot that can be loaded when starting the daemon. The `lotus chain export` command can be used to export a snapshot from the Lotus state for a specific height.

The following steps should be followed in every validator to recover a network from a crash or start a network from a specific network.
- Start the Lotus daemon with the corresponding state or snapshot of the Mir checkpoint from which the network wants to be restarted.
```
# Start daemon from snapshot
./lotus dameon --import-snapshot <snapshot car file>
```
- Once the lotus daemon has been initialized from the right state, we just need to start the mir-validator from the right checkpoint.
```
# Start validator from checkpoint
./eudico mir validator run --nosync --init-checkpoint=<checkpoint_file>
```
Once all `f+1` are restarted with the same state, you should start seeing how the validators start making progress from the height where they were initialized.
