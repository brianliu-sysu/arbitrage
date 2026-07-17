package blockchain

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type flashbotsRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type flashbotsRPCResponse struct {
	Result json.RawMessage    `json:"result"`
	Error  *flashbotsRPCError `json:"error"`
}

type flashbotsRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func submitFlashbotsBundles(
	ctx context.Context,
	relayURL string,
	authKey *ecdsa.PrivateKey,
	tx *types.Transaction,
	firstTargetBlock uint64,
	targetBlockCount uint64,
) error {
	rawTx, err := tx.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal flashbots transaction: %w", err)
	}
	txs := []string{hexutil.Encode(rawTx)}
	firstTarget := hexutil.EncodeUint64(firstTargetBlock)
	simulation, err := callFlashbotsRPC(ctx, relayURL, authKey, "eth_callBundle", []any{map[string]any{
		"txs":              txs,
		"blockNumber":      firstTarget,
		"stateBlockNumber": "latest",
	}})
	if err != nil {
		return fmt.Errorf("simulate flashbots bundle: %w", err)
	}
	if err := validateFlashbotsSimulation(simulation); err != nil {
		return fmt.Errorf("simulate flashbots bundle: %w", err)
	}

	for offset := uint64(0); offset < targetBlockCount; offset++ {
		target := hexutil.EncodeUint64(firstTargetBlock + offset)
		if _, err := callFlashbotsRPC(ctx, relayURL, authKey, "eth_sendBundle", []any{map[string]any{
			"txs":         txs,
			"blockNumber": target,
		}}); err != nil {
			return fmt.Errorf("submit flashbots bundle for block %s: %w", target, err)
		}
	}
	return nil
}

func validateFlashbotsSimulation(raw json.RawMessage) error {
	var result struct {
		Results []struct {
			Error  string `json:"error"`
			Revert string `json:"revert"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode simulation result: %w", err)
	}
	for index, txResult := range result.Results {
		if txResult.Error != "" {
			return fmt.Errorf("transaction %d: %s", index, txResult.Error)
		}
		if txResult.Revert != "" {
			return fmt.Errorf("transaction %d reverted: %s", index, txResult.Revert)
		}
	}
	return nil
}

func callFlashbotsRPC(
	ctx context.Context,
	relayURL string,
	authKey *ecdsa.PrivateKey,
	method string,
	params any,
) (json.RawMessage, error) {
	body, err := json.Marshal(flashbotsRPCRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	signature, err := flashbotsSignature(authKey, body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, relayURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Flashbots-Signature", signature)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("http status %d: %s", resp.StatusCode, responseBody)
	}
	var rpcResp flashbotsRPCResponse
	if err := json.Unmarshal(responseBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

func flashbotsSignature(authKey *ecdsa.PrivateKey, body []byte) (string, error) {
	if authKey == nil {
		return "", fmt.Errorf("flashbots auth key is required")
	}
	bodyHash := hexutil.Encode(crypto.Keccak256(body))
	signature, err := crypto.Sign(accounts.TextHash([]byte(bodyHash)), authKey)
	if err != nil {
		return "", fmt.Errorf("sign flashbots request: %w", err)
	}
	signature[64] += 27
	address := crypto.PubkeyToAddress(authKey.PublicKey)
	return address.Hex() + ":" + hexutil.Encode(signature), nil
}
