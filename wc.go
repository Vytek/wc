package wc

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	mathrand "math/rand"
	"net/http"
	"sync/atomic"

	"github.com/algorand/go-algorand-sdk/encoding/msgpack"
	"github.com/algorand/go-algorand-sdk/types"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

var letters = []rune("abcdefghijklmnopqrstuvwxyz")

const (
	jsonRpc20 = "2.0"

	sessionRequestMethod = "wc_sessionRequest"
	algoSignTxnMethod    = "algo_signTxn"
)

type Msg interface{}

type WC struct {
}

type SessionRequestPeerMeta struct {
	Description string   `json:"description"`
	Url         string   `json:"url"`
	Icons       []string `json:"icons"`
	Name        string   `json:"name"`
}

type SessionRequestParams struct {
	PeerId   string                 `json:"peerId"`
	PeerMeta SessionRequestPeerMeta `json:"peerMeta"`
	ChainId  int                    `json:"chainId"`
}

type Message struct {
	Topic   string `json:"topic"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Silent  bool   `json:"silent"`
}

type Request struct {
	Id      uint64        `json:"id"`
	JsonRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type SessionRequestResponseResult struct {
	PeerId   string                 `json:"peerId"`
	PeerMeta SessionRequestPeerMeta `json:"peerMeta"`
	Approved bool                   `json:"approved"`
	ChainId  int                    `json:"chainId"`
	Accounts []string               `json:"accounts"`
}

type Response struct {
	Id      uint64 `json:"id"`
	JsonRPC string `json:"jsonrpc"`
}

type SessionRequestResponse struct {
	Result SessionRequestResponseResult `json:"result"`
}

type AlgoSignParams struct {
	TxnBase64 string   `json:"txn"`
	Message   string   `json:"message"`
	Signers   []string `json:"signers,omitempty"`
}

type AlgoSignResponse struct {
	Result [][]byte `json:"result"`
}

type Client struct {
	id      uint64
	methods map[uint64]string

	host string
	conn *websocket.Conn
	key  []byte

	debug bool
}

type Option func(*Client)

func WithDebug(enabled bool) Option {
	return func(c *Client) {
		c.debug = enabled
	}
}

func WithHost(host string) Option {
	return func(c *Client) {
		c.host = host
	}
}

func WithKey(key []byte) Option {
	return func(c *Client) {
		c.key = key
	}
}

func MakeTopic() (string, error) {
	return uuid.NewString(), nil
}

func MakeClient(opts ...Option) (*Client, error) {
	header := make(http.Header)

	c := &Client{
		methods: make(map[uint64]string),
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.host == "" {
		sub := letters[mathrand.Int()%len(letters)]
		c.host = fmt.Sprintf("%c.bridge.walletconnect.org", sub)
	}

	if c.key == nil {
		key, err := MakeKey()
		if err != nil {
			return nil, errors.Wrap(err, "failed to make key")
		}
		c.key = key
	}

	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("wss://%s/", c.host), header)
	if err != nil {
		return nil, errors.Wrap(err, "failed to dial")
	}

	c.conn = conn

	return c, nil
}

func (c *Client) Subscribe(topic string) error {
	err := c.conn.WriteJSON(Message{
		Topic:   topic,
		Type:    "sub",
		Payload: "",
		Silent:  true,
	})

	if err != nil {
		return errors.Wrap(err, "failed to write")
	}

	return nil
}

func (c *Client) Send(topic string, req Request) error {
	c.methods[req.Id] = req.Method

	pb, err := json.Marshal(req)
	if err != nil {
		return errors.Wrap(err, "failed to marhsal")
	}

	if c.debug {
		fmt.Println("Send:", string(pb))
	}

	iv, err := MakeIV()
	if err != nil {
		return errors.Wrap(err, "failed to make iv")
	}

	emsg, err := encrypt(pb, c.key, iv)
	if err != nil {
		return errors.Wrap(err, "failed to encrypt")
	}

	cb, err := json.Marshal(emsg)
	if err != nil {
		return errors.Wrap(err, "failed to marshal")
	}

	bs := string(cb)

	msg := Message{
		Topic:   topic,
		Type:    "pub",
		Payload: bs,
		Silent:  true,
	}

	err = c.conn.WriteJSON(msg)
	if err != nil {
		return errors.Wrap(err, "failed to write")
	}

	return nil
}

func (c *Client) SendTransactions(peer string, txns []types.Transaction) error {
	params := make([]interface{}, len(txns))

	for i, txn := range txns {
		encoded := msgpack.Encode(txn)

		params[i] = AlgoSignParams{
			TxnBase64: base64.StdEncoding.EncodeToString(encoded),
			Message:   string(txn.Note),
		}
	}

	req := Request{
		Id:      atomic.AddUint64(&c.id, 1),
		JsonRPC: jsonRpc20,
		Method:  algoSignTxnMethod,
		Params:  []interface{}{params},
	}

	err := c.Send(peer, req)
	if err != nil {
		return errors.Wrap(err, "failed to write algo_signTxn")
	}

	return nil
}

func (c *Client) RequestSession(peer string, meta SessionRequestPeerMeta) (string, error) {
	req := Request{
		Id:      atomic.AddUint64(&c.id, 1),
		JsonRPC: jsonRpc20,
		Method:  sessionRequestMethod,
		Params: []interface{}{
			SessionRequestParams{
				PeerId:   peer,
				PeerMeta: meta,
				ChainId:  4160,
			},
		},
	}

	topic := uuid.New().String()

	err := c.Send(topic, req)
	if err != nil {
		return "", errors.Wrap(err, "failed to send request session")
	}

	keyString := hex.EncodeToString(c.key)

	url := fmt.Sprintf("wc:%s@1?bridge=https%%3A%%2F%%2F%s&key=%s", topic, c.host, keyString)

	if c.debug {
		fmt.Println("URL:", url)
	}

	return url, nil
}

func (c *Client) Read() (Msg, error) {
	_, p, err := c.conn.ReadMessage()
	if err != nil {
		return nil, errors.Wrap(err, "failed to read message")
	}

	var m Message
	err = json.Unmarshal(p, &m)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal")
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create cipher")
	}

	var e encrypted

	err = json.Unmarshal([]byte(m.Payload), &e)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal")
	}

	iv, err := hex.DecodeString(e.Iv)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode")
	}

	dec := cipher.NewCBCDecrypter(block, iv)

	ct, err := hex.DecodeString(e.Data)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode")
	}

	dec.CryptBlocks(ct, ct)

	var pt []byte
	if len(ct) > 0 {
		paddingSize := int(ct[len(ct)-1])
		pt = ct[0 : len(ct)-paddingSize]
	}

	var resp Response

	if c.debug {
		fmt.Println(string(pt))
	}

	err = json.Unmarshal(pt, &resp)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal response")
	}

	if resp.JsonRPC != jsonRpc20 {
		return nil, errors.New("unexpected jsonrpc version")
	}

	method := c.methods[resp.Id]
	delete(c.methods, resp.Id)

	if c.debug {
		fmt.Println("Id:", resp.Id, "| Method:", method)
	}

	switch method {
	case sessionRequestMethod:
		var resp SessionRequestResponse

		err = json.Unmarshal(pt, &resp)
		if err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal wc_sessionRequest response")
		}

		return resp, nil
	case algoSignTxnMethod:
		var resp AlgoSignResponse

		err = json.Unmarshal(pt, &resp)
		if err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal algo_signTxn response")
		}

		return resp, nil
	}

	return resp, nil
}
