// Package xrpl fetches a borrower's account history from the XRPL and reduces it to the four
// features the scoring model consumes.
//
// PRIVACY INVARIANT — the single most important property of this package:
// Everything here runs inside the TEE. Raw transaction data must never be logged, persisted, or
// returned beyond the Features struct. There is deliberately no logging in this file at all: the
// container's stdout is visible to whoever operates the machine, and a single Printf of a response
// body would leak exactly what TrustLine exists to protect.
//
// MVP NOTE: this queries the XRPL public JSON-RPC directly. That is sound inside a TEE (the operator
// cannot see the response) but it does mean the extension trusts the endpoint it queries. A
// production build would either pin a set of endpoints and require agreement, or route through an
// FDC attestation. FDC cannot replace this today: its attestation types are per-transaction, and
// Web2Json would publish the response on-chain in the clear, which defeats the privacy goal.
package xrpl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"trustline-fce/internal/scoring"
)

// rippleEpochOffset converts XRPL ledger time (seconds since 2000-01-01) to Unix seconds.
const rippleEpochOffset int64 = 946_684_800

// dropsPerXRP is the XRPL base unit ratio.
const dropsPerXRP = 1_000_000.0

// Client queries an XRPL JSON-RPC endpoint.
type Client struct {
	endpoint string
	http     *http.Client
}

func NewClient(endpoint string, timeout time.Duration) *Client {
	return &Client{endpoint: endpoint, http: &http.Client{Timeout: timeout}}
}

type rpcRequest struct {
	Method string `json:"method"`
	Params []any  `json:"params"`
}

type accountTxParams struct {
	Account     string `json:"account"`
	LedgerIndex string `json:"ledger_index_min,omitempty"`
	Limit       int    `json:"limit"`
	Binary      bool   `json:"binary"`
	Forward     bool   `json:"forward"`
}

type accountTxResponse struct {
	Result struct {
		Account      string `json:"account"`
		Status       string `json:"status"`
		ErrorMessage string `json:"error_message"`
		Error        string `json:"error"`
		Transactions []struct {
			Tx struct {
				TransactionType string          `json:"TransactionType"`
				Account         string          `json:"Account"`
				Destination     string          `json:"Destination"`
				Amount          json.RawMessage `json:"Amount"`
				Date            int64           `json:"date"`
			} `json:"tx"`
			Validated bool `json:"validated"`
		} `json:"transactions"`
	} `json:"result"`
}

// FetchFeatures pulls the account's transaction history and reduces it to scoring features.
//
// Returns zero-valued Features (which score to tier 0) for an account that does not exist, rather
// than an error — "no history" is a legitimate credit outcome, not a failure.
func (c *Client) FetchFeatures(ctx context.Context, address string) (scoring.Features, error) {
	const maxTx = 200 // matches scoring.ActivitySaturationCount; more would not change the score

	body, err := json.Marshal(rpcRequest{
		Method: "account_tx",
		Params: []any{accountTxParams{Account: address, Limit: maxTx, Binary: false, Forward: false}},
	})
	if err != nil {
		return scoring.Features{}, fmt.Errorf("encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return scoring.Features{}, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// Deliberately does not wrap the URL or response — keep failure messages free of borrower data.
		return scoring.Features{}, fmt.Errorf("xrpl request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return scoring.Features{}, fmt.Errorf("xrpl returned status %d", resp.StatusCode)
	}

	var parsed accountTxResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return scoring.Features{}, fmt.Errorf("decoding xrpl response: %w", err)
	}

	// An unfunded/unknown account is a valid tier-0 outcome, not an error.
	if parsed.Result.Error == "actNotFound" {
		return scoring.Features{}, nil
	}
	if parsed.Result.Error != "" {
		return scoring.Features{}, fmt.Errorf("xrpl error: %s", parsed.Result.Error)
	}

	return reduceToFeatures(address, parsed, time.Now().Unix()), nil
}

// reduceToFeatures is pure so it can be tested against fixtures without a network call.
func reduceToFeatures(address string, r accountTxResponse, nowUnix int64) scoring.Features {
	var (
		oldestDate     int64
		counterparties = make(map[string]struct{})
		volumeXRP      float64
		count          int
	)

	for _, entry := range r.Result.Transactions {
		if !entry.Validated {
			continue
		}
		count++
		tx := entry.Tx

		if tx.Date > 0 && (oldestDate == 0 || tx.Date < oldestDate) {
			oldestDate = tx.Date
		}

		// Count the *other* party, never the borrower themselves — otherwise every account would
		// show at least one counterparty and self-transfers would inflate diversity.
		if tx.Account != "" && tx.Account != address {
			counterparties[tx.Account] = struct{}{}
		}
		if tx.Destination != "" && tx.Destination != address {
			counterparties[tx.Destination] = struct{}{}
		}

		if tx.TransactionType == "Payment" {
			volumeXRP += parseDropAmount(tx.Amount)
		}
	}

	var ageDays float64
	if oldestDate > 0 {
		firstUnix := oldestDate + rippleEpochOffset
		if nowUnix > firstUnix {
			ageDays = float64(nowUnix-firstUnix) / 86400.0
		}
	}

	return scoring.Features{
		AccountAgeDays:         ageDays,
		TransactionCount:       count,
		PaymentVolumeXRP:       volumeXRP,
		DistinctCounterparties: len(counterparties),
	}
}

// parseDropAmount handles XRPL's dual Amount encoding: a JSON string of drops for XRP, or an object
// for issued currencies. Issued-currency amounts are ignored — they are not XRP and mixing them into
// an XRP volume figure would be misleading.
func parseDropAmount(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	var dropsStr string
	if err := json.Unmarshal(raw, &dropsStr); err != nil {
		return 0 // object form: issued currency, not XRP
	}
	drops, err := strconv.ParseFloat(dropsStr, 64)
	if err != nil {
		return 0
	}
	return drops / dropsPerXRP
}
