package wc

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

type Conn struct {
	host string
	conn *websocket.Conn
	key  []byte

	debug bool
}

type ConnOption func(*Conn)

func WithDebug(enabled bool) ConnOption {
	return func(c *Conn) {
		c.debug = enabled
	}
}

func WithHost(host string) ConnOption {
	return func(c *Conn) {
		c.host = host
	}
}

func WithKey(key []byte) ConnOption {
	return func(c *Conn) {
		c.key = key
	}
}

func MakeTopic() (string, error) {
	return uuid.NewString(), nil
}

func MakeConn(opts ...ConnOption) (*Conn, error) {
	header := make(http.Header)

	c := &Conn{}

	for _, opt := range opts {
		opt(c)
	}

	if c.host == "" {
		rand.Seed(time.Now().UnixNano())

		sub := letters[rand.Int()%len(letters)]
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

func (c *Conn) Subscribe(topic string) error {
	err := c.conn.WriteJSON(Message{
		Topic:   topic,
		Type:    "sub",
		Payload: "",
		Silent:  false,
	})

	if err != nil {
		return errors.Wrap(err, "failed to write")
	}

	return nil
}

func (c *Conn) Send(topic string, req Request) error {
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
		Silent:  false,
	}

	err = c.conn.WriteJSON(msg)
	if err != nil {
		return errors.Wrap(err, "failed to write")
	}

	return nil
}

func (c *Conn) MakeUrl(topic string) string {
	keyString := hex.EncodeToString(c.key)

	url := fmt.Sprintf("wc:%s@1?bridge=https%%3A%%2F%%2F%s&key=%s", topic, c.host, keyString)

	return url
}

func (c *Conn) Read() (Response, error) {
	_, p, err := c.conn.ReadMessage()
	if err != nil {
		return Response{}, errors.Wrap(err, "failed to read message")
	}

	var m Message
	err = json.Unmarshal(p, &m)
	if err != nil {
		return Response{}, errors.Wrap(err, "failed to unmarshal")
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return Response{}, errors.Wrap(err, "failed to create cipher")
	}

	var e encrypted

	err = json.Unmarshal([]byte(m.Payload), &e)
	if err != nil {
		return Response{}, errors.Wrap(err, "failed to unmarshal")
	}

	iv, err := hex.DecodeString(e.Iv)
	if err != nil {
		return Response{}, errors.Wrap(err, "failed to decode")
	}

	dec := cipher.NewCBCDecrypter(block, iv)

	ct, err := hex.DecodeString(e.Data)
	if err != nil {
		return Response{}, errors.Wrap(err, "failed to decode")
	}

	dec.CryptBlocks(ct, ct)

	var pb []byte
	if len(ct) > 0 {
		paddingSize := int(ct[len(ct)-1])
		pb = ct[0 : len(ct)-paddingSize]
	}

	var head ResponseHeader

	if c.debug {
		fmt.Println(string(pb))
	}

	err = json.Unmarshal(pb, &head)
	if err != nil {
		return Response{}, errors.Wrap(err, "failed to unmarshal response")
	}

	if head.JsonRPC != jsonRpc20 {
		return Response{}, errors.New("unexpected jsonrpc version")
	}

	if c.debug {
		fmt.Println("Id:", head.Id)
	}

	return Response{
		ResponseHeader: head,
		Result:         pb,
	}, nil
}
