package fxmodules

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"github.com/filecoin-project/go-jsonrpc"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/mitchellh/go-homedir"
	"github.com/multiformats/go-multiaddr"
	"github.com/urfave/cli/v2"
	"go.uber.org/fx"
	"golang.org/x/xerrors"
	"net/http"
	"os"
	"strings"
)

func RpcServer(cctx *cli.Context, r *repo.FsRepo, lr repo.LockedRepo, cfg *config.FullNode) fx.Option {
	if cctx.IsSet("api") {
		cfg.API.ListenAddress = "/ip4/127.0.0.1/tcp/" + cctx.String("api")
	}

	return fx.Module("startup",
		fx.Provide(
			startRPCServer,
			newFullNodeHandler,
			func(api impl.FullNodeAPI, lr repo.LockedRepo, e dtypes.APIEndpoint) lapi.FullNode {
				lr.SetAPIEndpoint(e)
				return &api
			},
			func() (dtypes.APIEndpoint, error) {
				return multiaddr.NewMultiaddr(cfg.API.ListenAddress)
			},
			func() (*types.KeyInfo, error) {
				if cctx.String("import-key") != "" {
					return importKey(cctx.String("import-key"))
				} else {
					return nil, nil
				}

			},
		),
		fx.Supply(
			r,
			lr,
			newServerOptions(cctx.Int("api-max-req-size")),
		),
	)
}

func newServerOptions(maxRequestSize int) []jsonrpc.ServerOption {
	serverOptions := []jsonrpc.ServerOption{jsonrpc.WithServerErrors(lapi.RPCErrors)}
	if maxRequestSize > 0 {
		serverOptions = append(serverOptions, jsonrpc.WithMaxRequestSize(int64(maxRequestSize)))
	}
	return serverOptions
}

func newFullNodeHandler(ctx context.Context, api lapi.FullNode, serverOptions []jsonrpc.ServerOption, importedKey *types.KeyInfo) (http.Handler, error) {
	if importedKey != nil {
		addr, err := api.WalletImport(ctx, importedKey)
		if err != nil {
			return nil, err
		}
		if err := api.WalletSetDefault(ctx, addr); err != nil {
			return nil, err
		}
	}
	return node.FullNodeHandler(api, true, serverOptions...)
}

func startRPCServer(h http.Handler, repo *repo.FsRepo) (node.StopFunc, error) {
	endpoint, err := repo.APIEndpoint()
	if err != nil {
		return nil, xerrors.Errorf("getting api endpoint: %w", err)
	}
	return node.ServeRPC(h, "lotus-daemon", endpoint)
}

func importKey(path string) (*types.KeyInfo, error) {
	path, err := homedir.Expand(path)
	if err != nil {
		return nil, err
	}

	hexdata, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	data, err := hex.DecodeString(strings.TrimSpace(string(hexdata)))
	if err != nil {
		return nil, err
	}

	var ki types.KeyInfo
	if err := json.Unmarshal(data, &ki); err != nil {
		return nil, err
	}

	return &ki, nil
}
