package wc

import (
	"encoding/base64"

	"github.com/algorand/go-algorand-sdk/encoding/msgpack"
	"github.com/algorand/go-algorand-sdk/types"
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

type RequestHeader struct {
	Id      uint64 `json:"id"`
	JsonRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
}

type SessionUpdateParams struct {
	Approved  bool     `json:"approved"`
	ChainId   int      `json:"chainId"`
	NetworkId int      `json:"networkId"`
	Accounts  []string `json:"accounts"`
}

type SessionUpdateRequest struct {
	RequestHeader
	Params []SessionUpdateParams `json:"params"`
}

type Request struct {
	RequestHeader
	Params []interface{} `json:"params"`
}

type SessionRequestResponseResult struct {
	PeerId   string                 `json:"peerId"`
	PeerMeta SessionRequestPeerMeta `json:"peerMeta"`
	Approved bool                   `json:"approved"`
	ChainId  int                    `json:"chainId"`
	Accounts []string               `json:"accounts"`
}

type ResponseHeader struct {
	Id      uint64 `json:"id"`
	JsonRPC string `json:"jsonrpc"`
}

type Response struct {
	ResponseHeader
	Result []byte
}

type SessionRequestResponse struct {
	Result SessionRequestResponseResult `json:"result"`
}

type AlgoSignParams struct {
	TxnBase64 string   `json:"txn"`
	Message   string   `json:"message"`
	Signers   []string `json:"signers,omitempty"`
}
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type AlgoSignResponse struct {
	Error  *Error   `json:"error"`
	Result [][]byte `json:"result"`
}

func MakeSignTransactions(id uint64, txns []types.Transaction) (Request, error) {
	params := make([]interface{}, len(txns))

	for i, txn := range txns {
		encoded := msgpack.Encode(txn)

		params[i] = AlgoSignParams{
			TxnBase64: base64.StdEncoding.EncodeToString(encoded),
			Message:   string(txn.Note),
		}
	}

	req := Request{
		RequestHeader: MakeHeader(id, algoSignTxnMethod),
		Params:        []interface{}{params},
	}

	return req, nil
}

func MakeHeader(id uint64, method string) RequestHeader {
	return RequestHeader{
		Id:      id,
		JsonRPC: jsonRpc20,
		Method:  method,
	}
}

func MakeRequestSession(id uint64, peer string, meta SessionRequestPeerMeta) (Request, error) {
	req := Request{
		RequestHeader: MakeHeader(id, sessionRequestMethod),
		Params: []interface{}{
			SessionRequestParams{
				PeerId:   peer,
				PeerMeta: meta,
				ChainId:  4160,
			},
		},
	}

	return req, nil
}
