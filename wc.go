package wc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/algorand/go-algorand-sdk/encoding/msgpack"
	"github.com/algorand/go-algorand-sdk/types"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
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

type encrypted struct {
	Data string `json:"data"`
	Hmac string `json:"hmac"`
	Iv   string `json:"iv"`
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
	Id uint64 `json:"id"`
}

type SessionRequestResponse struct {
	Id      int                          `json:"id"`
	JsonRPC string                       `json:"jsonrpc"`
	Result  SessionRequestResponseResult `json:"result"`
}

type AlgoSignParams struct {
	TxnBase64 string   `json:"txn"`
	Message   string   `json:"message"`
	Signers   []string `json:"signers,omitempty"`
}

type AlgoSignResponse struct {
	Id      int      `json:"id"`
	JsonRPC string   `json:"jsonrpc"`
	Result  [][]byte `json:"result"`
}

func encrypt(v interface{}, key []byte) (encrypted, error) {
	var r encrypted
	b, err := json.Marshal(v)
	if err != nil {
		return r, errors.Wrap(err, "failed to marhsal")
	}

	iv := make([]byte, 16)
	_, err = rand.Read(iv)
	if err != nil {
		return r, errors.Wrap(err, "failed to fill iv")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return r, errors.Wrap(err, "failed to create cipher")
	}

	padding := block.BlockSize() - len(b)%block.BlockSize()
	ct := make([]byte, len(b)+padding)
	copy(ct, b)

	for i := len(b); i < len(b)+padding; i++ {
		ct[i] = byte(padding)
	}

	enc := cipher.NewCBCEncrypter(block, iv)
	enc.CryptBlocks(ct, ct)

	hm := hmac.New(sha256.New, key)

	hmd := make([]byte, len(ct)+len(iv))

	copy(hmd[0:], ct)
	copy(hmd[len(ct):], iv)

	_, err = hm.Write(hmd)
	if err != nil {
		return r, errors.Wrap(err, "failed to write")
	}

	hmacsha256 := hm.Sum(nil)

	r = encrypted{
		Data: hex.EncodeToString(ct),
		Hmac: hex.EncodeToString(hmacsha256),
		Iv:   hex.EncodeToString(iv),
	}

	return r, nil
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

func MakeKey() ([]byte, error) {
	key := make([]byte, 32)

	_, err := rand.Read(key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make key")
	}

	return key, nil
}

func MakeTopic() (string, error) {
	return uuid.NewString(), nil
}

func Dial(host string, key []byte, opts ...Option) (*Client, error) {
	header := make(http.Header)

	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("wss://%s/", host), header)
	if err != nil {
		return nil, errors.Wrap(err, "failed to dial")
	}

	c := &Client{
		methods: make(map[uint64]string),

		host: host,
		conn: conn,
		key:  key,
	}

	for _, opt := range opts {
		opt(c)
	}

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
		JsonRPC: "2.0",
		Method:  "algo_signTxn",
		Params:  []interface{}{params},
	}

	c.methods[req.Id] = req.Method

	emsg, err := encrypt(req, c.key)
	if err != nil {
		return errors.Wrap(err, "failed to encrypt")
	}

	b, err := json.Marshal(emsg)
	if err != nil {
		return errors.Wrap(err, "failed to marshal")
	}

	bs := string(b)

	msg := Message{
		Topic:   peer,
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

func (c *Client) RequestSession(topic string, meta SessionRequestPeerMeta) (string, error) {
	req := Request{
		Id:      atomic.AddUint64(&c.id, 1),
		JsonRPC: "2.0",
		Method:  "wc_sessionRequest",
		Params: []interface{}{
			SessionRequestParams{
				PeerId:   topic,
				PeerMeta: meta,
				ChainId:  4160,
			},
		},
	}

	c.methods[req.Id] = req.Method

	emsg, err := encrypt(req, c.key)
	if err != nil {
		return "", errors.Wrap(err, "failed to encrypt")
	}

	b, err := json.Marshal(emsg)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal")
	}

	bs := string(b)

	pubTopic := uuid.New().String()

	msg := Message{
		Topic:   pubTopic,
		Type:    "pub",
		Payload: bs,
		Silent:  true,
	}

	err = c.conn.WriteJSON(msg)
	if err != nil {
		return "", errors.Wrap(err, "failed to write")
	}

	keyString := hex.EncodeToString(c.key)

	url := fmt.Sprintf("wc:%s@1?bridge=https%%3A%%2F%%2F%s&key=%s", pubTopic, c.host, keyString)

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

	method := c.methods[resp.Id]
	delete(c.methods, resp.Id)

	if c.debug {
		fmt.Println("Id:", resp.Id, "| Method:", method)
	}

	switch method {
	case "wc_sessionRequest":
		var resp SessionRequestResponse

		err = json.Unmarshal(pt, &resp)
		if err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal wc_sessionRequest response")
		}

		return resp, nil
	case "algo_signTxn":
		var resp AlgoSignResponse

		err = json.Unmarshal(pt, &resp)
		if err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal algo_signTxn response")
		}

		return resp, nil
	}

	return resp, nil
}
