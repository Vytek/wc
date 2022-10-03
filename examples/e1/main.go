package main

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"github.com/algorand/go-algorand-sdk/client/v2/algod"
	"github.com/algorand/go-algorand-sdk/transaction"
	"github.com/algorand/go-algorand-sdk/types"
	"github.com/dragmz/tqr"
	"github.com/dragmz/wc"
	"github.com/pkg/errors"
)

func run() error {
	c, err := wc.MakeClient(wc.WithDebug(true))
	if err != nil {
		return errors.Wrap(err, "failed to dial")
	}

	topic, err := wc.MakeTopic()
	if err != nil {
		return errors.Wrap(err, "failed to make topic")
	}

	url, err := c.RequestSession(topic, wc.SessionRequestPeerMeta{
		Name:        "wc",
		Description: "WalletConnect Go",
	})
	if err != nil {
		return errors.Wrap(err, "failed to request session")
	}

	err = c.Subscribe(topic)
	if err != nil {
		return errors.Wrap(err, "failed to subscribe")
	}

	fmt.Println(url)

	qr := tqr.New(url)
	fmt.Println(qr)

	ac, err := algod.MakeClient("https://mainnet-api.algonode.cloud", "")
	if err != nil {
		return errors.Wrap(err, "failed to create algod client")
	}

	for {
		msg, err := c.Read()
		if err != nil {
			return errors.Wrap(err, "failed to read message")
		}

		switch msg := msg.(type) {
		case wc.AlgoSignResponse:
			for _, tx := range msg.Result {
				id, err := ac.SendRawTransaction(tx).Do(context.Background())
				if err != nil {
					return errors.Wrap(err, "failed to send tx")
				}

				fmt.Println("Sent tx:", id)
				return nil
			}

		case wc.SessionRequestResponse:
			if msg.Result.Accounts != nil && len(msg.Result.Accounts) > 0 {
				fmt.Println("Press Enter to send test transaction..")

				r := bufio.NewReader(os.Stdin)
				r.ReadString('\n')

				sp, err := ac.SuggestedParams().Do(context.Background())
				if err != nil {
					return errors.Wrap(err, "failed to get suggested params")
				}

				tx, err := transaction.MakePaymentTxnWithFlatFee(msg.Result.Accounts[0], msg.Result.Accounts[0],
					transaction.MinTxnFee, 0, uint64(sp.FirstRoundValid), uint64(sp.LastRoundValid), []byte("test transaction"), "", sp.GenesisID, sp.GenesisHash)
				if err != nil {
					return errors.Wrap(err, "failed to make payment tx")
				}

				err = c.SendTransactions(msg.Result.PeerId, []types.Transaction{tx})
				if err != nil {
					return errors.Wrap(err, "failed to send payment tx")
				}
			}
		}
	}
}

func main() {
	err := run()
	if err != nil {
		panic(err)
	}
}
