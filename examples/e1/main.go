package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/algorand/go-algorand-sdk/client/v2/algod"
	"github.com/algorand/go-algorand-sdk/transaction"
	"github.com/algorand/go-algorand-sdk/types"
	"github.com/dragmz/tqr"
	"github.com/dragmz/wc"
	"github.com/pkg/errors"
)

type args struct {
	Algod      string
	AlgodToken string

	Debug bool
}

func run(a args) error {
	ac, err := algod.MakeClient(a.Algod, a.AlgodToken)
	if err != nil {
		return errors.Wrap(err, "failed to create algod client")
	}

	c, err := wc.MakeConn(wc.WithDebug(a.Debug))
	if err != nil {
		return errors.Wrap(err, "failed to dial")
	}

	var peer *wc.Peer
	var e *wc.Client

	e, err = wc.MakeClient(c,
		wc.WithUrlHandler(func(url string) error {
			qr := tqr.New(url)
			fmt.Println(qr)
			return nil
		}))

	if err != nil {
		return errors.Wrap(err, "failed to make wc engine")
	}

	peer, res, err := e.RequestSession(wc.SessionRequestPeerMeta{
		Name:        "wc",
		Description: "WalletConnect example 1",
	})
	if err != nil {
		return errors.Wrap(err, "failed to request session")
	}

	if len(res.Accounts) == 0 {
		return errors.New("no accounts")
	}

	account := res.Accounts[0]

	for {
		err := func() error {
			fmt.Printf("Session established - Account: %s. Press Enter to send test transaction..\n", account)

			rdr := bufio.NewReader(os.Stdin)
			rdr.ReadString('\n')

			sp, err := ac.SuggestedParams().Do(context.Background())
			if err != nil {
				return errors.Wrap(err, "failed to get suggested params")
			}

			tx, err := transaction.MakePaymentTxnWithFlatFee(account, account,
				transaction.MinTxnFee, 0, uint64(sp.FirstRoundValid), uint64(sp.LastRoundValid), []byte("test transaction"), "", sp.GenesisID, sp.GenesisHash)
			if err != nil {
				return errors.Wrap(err, "failed to make payment tx")
			}

			raws, err := peer.SignTransactions([]types.Transaction{tx})
			if err != nil {
				return errors.Wrap(err, "failed to send txs")
			}

			for _, raw := range raws {
				id, err := ac.SendRawTransaction(raw).Do(context.Background())
				if err != nil {
					return errors.Wrap(err, "failed to send raw tx")
				}

				fmt.Println("Sent tx:", id)
			}

			return nil
		}()

		if err != nil {
			fmt.Println("Error:", err)
		}
	}
}

func main() {
	var a args

	flag.StringVar(&a.Algod, "algod", "https://mainnet-api.algonode.cloud", "algod address")
	flag.StringVar(&a.AlgodToken, "algod-token", "", "algod token")
	flag.BoolVar(&a.Debug, "debug", false, "enable debug")

	flag.Parse()

	err := run(a)
	if err != nil {
		panic(err)
	}
}
