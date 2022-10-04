package wc

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/algorand/go-algorand-sdk/types"
	"github.com/pkg/errors"
)

type Client struct {
	c *Conn

	handlers map[uint64]func(r Response) error
	id       uint64

	urlHandler func(url string) error

	debug bool
}

type ClientOption func(e *Client)

func WithUrlHandler(f func(url string) error) ClientOption {
	return func(e *Client) {
		e.urlHandler = f
	}
}

func MakeClient(c *Conn, opts ...ClientOption) (*Client, error) {
	e := &Client{
		c:        c,
		handlers: make(map[uint64]func(r Response) error),
	}

	for _, opt := range opts {
		opt(e)
	}

	go func() {
		err := func() error {
			for {
				resp, err := e.c.Read()
				if err != nil {
					return errors.Wrap(err, "failed to read")
				}

				handler, ok := e.handlers[resp.Id]
				if ok {
					delete(e.handlers, resp.Id)
				}

				go func() {
					err := func() error {
						if ok {
							err = handler(resp)
							if err != nil {
								fmt.Println("Error:", err)
							}
						} else {
							if e.debug {
								fmt.Println("Unhandled message - Id:", resp.Id, "| JsonRPC:", resp.JsonRPC, "| Data:", string(resp.Result))
							}

							var head RequestHeader

							err := json.Unmarshal(resp.Result, &head)
							if err != nil {
								return errors.Wrap(err, "failed to unmarshal request")
							}

							switch head.Method {
							case "wc_sessionUpdate":
								var update SessionUpdateRequest

								fmt.Println("Update:", string(resp.Result))

								err = json.Unmarshal(resp.Result, &update)
								if err != nil {
									return errors.Wrapf(err, "failed to unmarshal: %s", head.Method)
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
		}()

		if err != nil {
			panic(err)
		}
	}()

	return e, nil
}

type Peer struct {
	e  *Client
	id string
}

func (e *Client) RequestSession(meta SessionRequestPeerMeta) (*Peer, SessionRequestResponseResult, error) {
	var r SessionRequestResponseResult

	peer, err := MakeTopic()
	if err != nil {
		return nil, r, errors.Wrap(err, "failed to make peer")
	}

	id := atomic.AddUint64(&e.id, 1)

	req, err := MakeRequestSession(id, peer, meta)
	if err != nil {
		return nil, r, errors.Wrap(err, "failed to make session request")
	}

	topic, err := MakeTopic()
	if err != nil {
		return nil, r, errors.Wrap(err, "failed to make topic")
	}

	err = e.c.Send(topic, req)
	if err != nil {
		return nil, r, errors.Wrap(err, "failed to send request session request")
	}

	url := e.c.MakeUrl(topic)

	if e.urlHandler != nil {
		err = e.urlHandler(url)
		if err != nil {
			return nil, r, errors.Wrap(err, "failed to handle url")
		}
	}

	ch := make(chan SessionRequestResponse)

	e.handlers[id] = func(r Response) error {
		var resp SessionRequestResponse

		err := json.Unmarshal(r.Result, &resp)
		if err != nil {
			return errors.Wrap(err, "failed to unmarshal wc_sessionRequest response")
		}

		ch <- resp

		return nil
	}

	err = e.c.Subscribe(peer)
	if err != nil {
		return nil, r, errors.Wrap(err, "failed to subscribe")
	}

	res := <-ch

	return &Peer{
		e:  e,
		id: topic,
	}, res.Result, nil
}

func (p *Peer) SignTransactions(txs []types.Transaction) ([][]byte, error) {
	id := atomic.AddUint64(&p.e.id, 1)

	req, err := MakeSignTransactions(id, txs)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make send transactions request")
	}

	err = p.e.c.Send(p.id, req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to send transactions request")
	}

	ch := make(chan AlgoSignResponse)

	p.e.handlers[p.e.id] = func(r Response) error {
		var resp AlgoSignResponse

		err := json.Unmarshal(r.Result, &resp)
		if err != nil {
			return errors.Wrap(err, "failed to unmarshal algo_signTxn response")
		}

		ch <- resp

		return nil
	}

	resp := <-ch

	if resp.Error != nil {
		err = errors.Errorf("Sign error - Code: %d, Message: %s", resp.Error.Code, resp.Error.Message)

		if p.e.debug {
			fmt.Println(err)
		}

		return nil, err
	}

	return resp.Result, nil
}
