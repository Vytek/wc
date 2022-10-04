package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync/atomic"

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
	var id uint64
	var accounts []string

	handlers := map[uint64]func(r wc.Response) error{}

	c, err := wc.MakeClient(wc.WithDebug(a.Debug))
	if err != nil {
		return errors.Wrap(err, "failed to dial")
	}

	peer, err := wc.MakeTopic()
	if err != nil {
		return errors.Wrap(err, "failed to make peer")
	}

	req, err := wc.MakeRequestSession(atomic.AddUint64(&id, 1), peer, wc.SessionRequestPeerMeta{
		Name:        "wc",
		Description: "WalletConnect Go",
	})

	if err != nil {
		return errors.Wrap(err, "failed to make request session request")
	}

	topic, err := wc.MakeTopic()
	if err != nil {
		return errors.Wrap(err, "failed to make topic")
	}

	err = c.Send(topic, req)
	if err != nil {
		return errors.Wrap(err, "failed to send request session request")
	}

	url := c.MakeUrl(topic)

	if a.Debug {
		fmt.Println("URL:", url)
	}

	ac, err := algod.MakeClient(a.Algod, a.AlgodToken)
	if err != nil {
		return errors.Wrap(err, "failed to create algod client")
	}

	handlers[id] = func(r wc.Response) error {
		var resp wc.SessionRequestResponse

		err := json.Unmarshal(r.Result, &resp)
		if err != nil {
			return errors.Wrap(err, "failed to unmarshal wc_sessionRequest response")
		}

		accounts = resp.Result.Accounts

		for {
			if accounts == nil || len(accounts) == 0 {
				fmt.Println("Session closed.")
				return nil
			}

			fmt.Printf("Session established - Account: %s. Press Enter to send test transaction..\n", resp.Result.Accounts[0])

			rdr := bufio.NewReader(os.Stdin)
			rdr.ReadString('\n')

			sp, err := ac.SuggestedParams().Do(context.Background())
			if err != nil {
				return errors.Wrap(err, "failed to get suggested params")
			}

			tx, err := transaction.MakePaymentTxnWithFlatFee(resp.Result.Accounts[0], resp.Result.Accounts[0],
				transaction.MinTxnFee, 0, uint64(sp.FirstRoundValid), uint64(sp.LastRoundValid), []byte("test transaction"), "", sp.GenesisID, sp.GenesisHash)
			if err != nil {
				return errors.Wrap(err, "failed to make payment tx")
			}

			req, err := wc.MakeSendTransactions(atomic.AddUint64(&id, 1), []types.Transaction{tx})
			if err != nil {
				return errors.Wrap(err, "failed to make send transactions request")
			}

			err = c.Send(resp.Result.PeerId, req)
			if err != nil {
				return errors.Wrap(err, "failed to send transactions request")
			}

			handlers[id] = func(r wc.Response) error {
				var resp wc.AlgoSignResponse

				err := json.Unmarshal(r.Result, &resp)
				if err != nil {
					return errors.Wrap(err, "failed to unmarshal algo_signTxn response")
				}

				if resp.Error != nil {
					fmt.Println("Sign error - Code", resp.Error.Code, "| Message:", resp.Error.Message)
				} else {
					for _, tx := range resp.Result {
						if a.Debug {
							fmt.Println("Sending raw tx - Data:", tx)
						}

						id, err := ac.SendRawTransaction(tx).Do(context.Background())
						if err != nil {
							return errors.Wrap(err, "failed to send tx")
						}

						fmt.Println("Sent tx:", id)
					}
				}
				return nil
			}
		}
	}

	err = c.Subscribe(peer)
	if err != nil {
		return errors.Wrap(err, "failed to subscribe")
	}

	qr := tqr.New(url)
	fmt.Println(qr)

	for {
		msg, err := c.Read()
		if err != nil {
			return errors.Wrap(err, "failed to read message")
		}

		handler, ok := handlers[msg.Id]

		go func() {
			err := func() error {
				if ok {
					delete(handlers, msg.Id)

					err = handler(msg)
					if err != nil {
						fmt.Println("Error:", err)
					}
				} else {
					fmt.Println("Unhandled message - Id:", msg.Id, "| JsonRPC:", msg.JsonRPC, "| Data:", string(msg.Result))

					var head wc.RequestHeader

					err := json.Unmarshal(msg.Result, &head)
					if err != nil {
						return errors.Wrap(err, "failed to unmarshal request")
					}

					switch head.Method {
					case "wc_sessionUpdate":
						var update wc.SessionUpdateRequest

						err = json.Unmarshal(msg.Result, &update)
						if err != nil {
							return errors.Wrapf(err, "failed to unmarshal: %s", head.Method)
						}

						for _, p := range update.Params {
							fmt.Println("Session update - Approved:", p.Approved, "| Accounts:", p.Accounts)
							accounts = p.Accounts
						}
					}
				}

				return nil
			}()

			if err != nil {
				fmt.Println("Handler error:", err)
			}
		}()
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
