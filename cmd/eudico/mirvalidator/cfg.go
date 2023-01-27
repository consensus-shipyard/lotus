package mirvalidator

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/chain/consensus/mir/validator"
	lcli "github.com/filecoin-project/lotus/cli"
)

// TODO: Make these config files configurable.
const (
	PrivKeyPath       = "mir.key"
	MaddrPath         = "mir.maddr"
	MembershipCfgPath = "mir.validators"
	LevelDSPath       = "mir.db"
)

var configFiles = []string{PrivKeyPath, MaddrPath, MembershipCfgPath, LevelDSPath}

var cfgCmd = &cli.Command{
	Name:  "config",
	Usage: "Interact Mir validator config",
	Subcommands: []*cli.Command{
		initCmd,
		addValidatorCmd,
		validatorAddrCmd,
	},
}

var addValidatorCmd = &cli.Command{
	Name:  "add-validator",
	Usage: "Add validator to mir membership configuration",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 1 {
			return fmt.Errorf("expected validator address as input")
		}
		// check if repo initialized
		if err := repoInitialized(context.Background(), cctx); err != nil {
			return err
		}

		// check if validator has been initialized.
		if err := initCheck(cctx.String("repo")); err != nil {
			return err
		}

		membershipFile := path.Join(cctx.String("repo"), MembershipCfgPath)
		v, err := validator.NewValidatorFromString(cctx.Args().First())
		if err != nil {
			return fmt.Errorf("error parsing validator from string: %s. Use the following format: <wallet id>@<multiaddr>", err)
		}

		if err := validator.AddValidatorToFile(membershipFile, v); err != nil {
			return fmt.Errorf("failed to add validator to file %s: %w", membershipFile, err)
		}

		log.Infow("Mir validator added to membership file")
		return nil
	},
}

var validatorAddrCmd = &cli.Command{
	Name:  "validator-addr",
	Usage: "Output the validator address formatted to populate membership config",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "default-key",
			Value: true,
			Usage: "use default wallet's key",
		},
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account used for the validator",
		},
	},
	Action: func(cctx *cli.Context) error {
		// check if repo initialized
		if err := repoInitialized(context.Background(), cctx); err != nil {
			return err
		}

		// check if validator has been initialized.
		if err := initCheck(cctx.String("repo")); err != nil {
			return err
		}

		nodeApi, ncloser, err := lcli.GetFullNodeAPIV1(cctx)
		if err != nil {
			return xerrors.Errorf("getting full node api: %w", err)
		}
		defer ncloser()

		// validator identity.
		validator, err := validatorIDFromFlag(context.Background(), cctx, nodeApi)
		if err != nil {
			return err
		}

		pk, err := lp2pID(cctx.String("repo"))
		if err != nil {
			return fmt.Errorf("error getting libp2p private key: %s", err)
		}
		pid, err := peer.IDFromPublicKey(pk.GetPublic())
		if err != nil {
			return fmt.Errorf("error generating ID from private key: %s", err)
		}

		// get multiaddr for host.
		path := filepath.Join(cctx.String("repo"), MaddrPath)
		bMaddr, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("error reading multiaddr from file: %w", err)
		}
		addrs, err := unmarshalMultiAddrSlice(bMaddr)
		if err != nil {
			return err
		}

		for _, a := range addrs {
			fmt.Printf("%s@%s/p2p/%s\n", validator, a, pid)
		}

		return nil
	},
}

func cleanConfig(repo string) {
	log.Infow("Cleaning mir config files from repo")
	for _, s := range configFiles {
		p := filepath.Join(repo, s)
		err := os.Remove(p)
		if err != nil {
			log.Warnf("error cleaning config file %s: %s", s, err)
		}
	}
}
