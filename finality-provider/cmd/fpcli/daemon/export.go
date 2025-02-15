package daemon

import (
	"context"
	"encoding/hex"
	"fmt"

	"cosmossdk.io/math"
	fpcfg "github.com/babylonchain/finality-provider/finality-provider/config"
	dc "github.com/babylonchain/finality-provider/finality-provider/service/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/x/staking/types"

	bbn "github.com/babylonchain/babylon/types"
	btcstktypes "github.com/babylonchain/babylon/x/btcstaking/types"

	"github.com/urfave/cli"
)

// FinalityProviderSigned wraps the finality provider by adding the
// signature signed by the finality provider's Babylon key in hex
type FinalityProviderSigned struct {
	btcstktypes.FinalityProvider
	// FpSigHex is the finality provider cosmos sdk chain key
	// can be verified with the pub key in btcstktypes.FinalityProvider.BabylonPk
	FpSigHex string `json:"fp_sig_hex"`
}

var ExportFinalityProvider = cli.Command{
	Name:      "export-finality-provider",
	ShortName: "exfp",
	Usage:     "It exports the finality provider by the given BTC public key.",
	Description: `Fetches the finality provider from the database and exports it
	by printing the json structure on the stdout.`,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  fpdDaemonAddressFlag,
			Usage: "The RPC server address of fpd",
			Value: defaultFpdDaemonAddress,
		},
		cli.StringFlag{
			Name:     fpBTCPkFlag,
			Usage:    "The hex string of the BTC public key",
			Required: true,
		},
		cli.BoolFlag{
			Name: signedFlag,
			Usage: `Specify if the exported finality provider information should be signed,
			if true, it will sign using the flag key-name, if not set it will load from the
			babylon key on config.`,
		},
		cli.StringFlag{
			Name:  keyNameFlag,
			Usage: "The unique name of the finality provider key",
		},
		cli.StringFlag{
			Name:  homeFlag,
			Usage: "The home path of the finality provider daemon (fpd)",
			Value: fpcfg.DefaultFpdDir,
		},
		cli.StringFlag{
			Name:  passphraseFlag,
			Usage: "The pass phrase used to encrypt the keys",
			Value: defaultPassphrase,
		},
		cli.StringFlag{
			Name:  hdPathFlag,
			Usage: "The hd path used to derive the private key",
			Value: defaultHdPath,
		},
	},
	Action: exportFp,
}

func exportFp(ctx *cli.Context) error {
	daemonAddress := ctx.String(fpdDaemonAddressFlag)
	client, cleanUp, err := dc.NewFinalityProviderServiceGRpcClient(daemonAddress)
	if err != nil {
		return fmt.Errorf("failled to connect to daemon addr %s: %w", daemonAddress, err)
	}
	defer cleanUp()

	fpBtcPkHex := ctx.String(fpBTCPkFlag)
	fpPk, err := bbn.NewBIP340PubKeyFromHex(fpBtcPkHex)
	if err != nil {
		return fmt.Errorf("invalid fp btc pk hex %s: %w", fpBtcPkHex, err)
	}

	fpInfoResp, err := client.QueryFinalityProviderInfo(context.Background(), fpPk)
	if err != nil {
		return fmt.Errorf("failed to query fp info from %s: %w", fpBtcPkHex, err)
	}

	fpInfo := fpInfoResp.FinalityProvider
	comm, err := math.LegacyNewDecFromStr(fpInfo.Commission)
	if err != nil {
		return fmt.Errorf("failed to parse fp commission %s: %w", fpInfo.Commission, err)
	}

	cosmosRawPubKey, err := hex.DecodeString(fpInfo.ChainPkHex)
	if err != nil {
		return fmt.Errorf("failed to decode chain pk hex %s: %w", fpInfo.ChainPkHex, err)
	}

	cosmosPubKey := &secp256k1.PubKey{
		Key: cosmosRawPubKey,
	}

	desc := fpInfo.Description
	fp := btcstktypes.FinalityProvider{
		Description: &types.Description{
			Moniker:         desc.Moniker,
			Identity:        desc.Identity,
			Website:         desc.Website,
			SecurityContact: desc.SecurityContact,
			Details:         desc.Details,
		},
		Commission: &comm,
		BtcPk:      fpPk,
		BabylonPk:  cosmosPubKey,
		Pop:        nil, // TODO: fill PoP?
	}

	if !ctx.Bool(signedFlag) {
		printRespJSON(fp)
		return nil
	}

	keyName, err := loadKeyName(ctx)
	if err != nil {
		return fmt.Errorf("not able to load key name: %w", err)
	}

	// sign the finality provider data.
	fpbz, err := fp.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal finality provider %+v: %w", fp, err)
	}

	resp, err := client.SignMessageFromChainKey(
		context.Background(),
		keyName,
		ctx.String(passphraseFlag),
		ctx.String(hdPathFlag),
		fpbz,
	)
	if err != nil {
		return fmt.Errorf("failed to sign finality provider: %w", err)
	}

	printRespJSON(FinalityProviderSigned{
		FinalityProvider: fp,
		FpSigHex:         hex.EncodeToString(resp.Signature),
	})

	return nil
}
