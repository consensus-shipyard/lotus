package mirvalidator

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/consensus-shipyard/go-ipc-types/validator"
	"github.com/urfave/cli/v2"
)

var initCmd = &cli.Command{
	Name:  "init",
	Usage: "Initialize the config for a mir-validator",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "overwrite",
			Usage:   "Overwrites existing validator config from repo. Use with caution!",
			Aliases: []string{"f"},
			Value:   false,
		},
		&cli.StringFlag{
			Name:  "membership",
			Usage: "Pass membership config with the list of validator in the validator set",
		},
		&cli.IntFlag{
			Name:  "tcp-libp2p-port",
			Usage: "Listening TCP libp2p port",
			Value: DefaultTCPLibP2PPort,
		},
		&cli.IntFlag{
			Name:  "quic-libp2p-port",
			Usage: "Listening QUIC libp2p port",
			Value: DefaultQuicLibP2PPort,
		},
	},
	Action: func(cctx *cli.Context) error {
		// check if repo initialized
		if err := repoInitialized(context.Background(), cctx); err != nil {
			return err
		}
		if cctx.Bool("overwrite") {
			cleanConfig(cctx.String("repo"))
		} else {
			// check if validator has been initialized.
			isCfg, err := isConfigured(cctx.String("repo"))
			if err == nil {
				return fmt.Errorf("validator already configured. Run `./eudico mir validator config init -f` if you want to overwrite the current config")
			}
			if isCfg && err != nil {
				return fmt.Errorf("validator configured and config corrupted: %v. Backup the config files you want to keep and run `./eudico mir validator init -f`", err)
			}
		}

		_, err := newLibP2PHost(cctx.String("repo"), cctx.Int("tcp-libp2p-port"), cctx.Int("quic-libp2p-port"))
		if err != nil {
			return fmt.Errorf("couldn't initialize libp2p config: %s", err)
		}

		var validators *validator.Set
		// TODO: Pass validator set for initialization
		mp := path.Join(cctx.String("repo"), MembershipCfgPath)
		if cctx.String("membership") != "" {
			validators, err = validator.NewValidatorSetFromFile(cctx.String("membership"))
			if err != nil {
				return fmt.Errorf("error importing membership config specified: %s", err)
			}
		} else {
			log.Infof("Creating empty membership cfg at %s. Remember to run ./eudico mir validator add-validator to add more membership validators", mp)
			if _, err := os.Create(mp); err != nil {
				return fmt.Errorf("error creating empty membership config in %s", mp)
			}
			validators = validator.NewEmptyValidatorSet()
		}

		// persist validator config in the right path.
		if err := validators.Save(mp); err != nil {
			return fmt.Errorf("error exporting membership config: %s", err)
		}

		// initialize database path
		datastoreCfg := filepath.Join(cctx.String("repo"), LevelDSPath)
		if err := os.MkdirAll(datastoreCfg, 0755); err != nil {
			return fmt.Errorf("error initializing mir datastore in path %s: %s", LevelDSPath, err)
		}

		log.Infow("Initialized mir validator. run ./eudico mir validator run to start validator process")
		return nil
	},
}
