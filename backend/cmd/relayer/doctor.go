package main

// Diagnostics for "the instruction dispatched on-chain but nothing reached my proxy".
//
// The delivery model is the thing most people get wrong, so it is worth stating plainly:
// data providers POST the cosigned instruction DIRECTLY to your proxy at /instruction. Your proxy
// does NOT discover instructions by reading the indexer. So delivery depends entirely on the
// machine's on-chain record being correct and your endpoint being reachable, and a 404 from
// /action/result usually means the instruction never arrived rather than that the proxy is down.
//
// This command checks, in the order that fails most often:
//  1. you are talking to the LIVE FlareTeeManager, not the dead pre-22-Jul one
//  2. the teeId is registered, and to which extension
//  3. status is 2 (PRODUCTION), not 1 (INITIALIZED)
//  4. the availability check is fresh (<6h)
//  5. the URL stored on-chain is the one you are actually serving, and is stable HTTPS
//  6. that URL answers GET /info

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ethereumCall builds a read-only call to the diamond.
func ethereumCall(to common.Address, data []byte) ethereum.CallMsg {
	return ethereum.CallMsg{To: &to, Data: data}
}

// LiveFlareTeeManager is the current Coston2 deployment. Every TEE facet is behind this one
// EIP-2535 diamond.
const LiveFlareTeeManager = "0x1a9C4A0f9D76c0b1D91d22E24E573a9b377618aE"

// DeadFlareTeeManager is the pre-2026-07-22 deployment. A stack still pointed here is the single
// most common cause of FunctionNotFound and reverting register() calls.
const DeadFlareTeeManager = "0x004224fa0b1a4d5c0f9b6a0e9e0f0d0e0c0b5d41F"

// TEE machine lifecycle statuses. 1 and 2 are confirmed; higher values exist for paused/banned
// (the registry exposes ban/unban/pause/toProduction) but are not pinned here because we have not
// confirmed their numbering — anything that is not 2 is treated as not-trustworthy.
const (
	StatusInitialized uint8 = 1
	StatusProduction  uint8 = 2
)

// availabilityFreshness is the availability-check validity window. Confirmed on Coston2:
// getSettings() returns availabilityCheckValidityDurationSeconds = 21600 = exactly 6h.
const availabilityFreshness = 6 * time.Hour

const doctorABI = `[
 {"inputs":[{"name":"_teeId","type":"address"}],"name":"getTeeMachineStatus","outputs":[{"type":"uint8"}],"stateMutability":"view","type":"function"},
 {"inputs":[{"name":"_teeId","type":"address"}],"name":"getExtensionId","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
 {"inputs":[{"name":"_teeId","type":"address"}],"name":"getTeeMachine","outputs":[{"components":[{"name":"teeId","type":"address"},{"name":"teeProxyId","type":"address"},{"name":"url","type":"string"}],"type":"tuple"}],"stateMutability":"view","type":"function"},
 {"inputs":[{"name":"_teeId","type":"address"}],"name":"getAvailabilityCheckValidity","outputs":[{"name":"_endTs","type":"uint64"},{"name":"_lastSigningPolicyId","type":"uint32"}],"stateMutability":"view","type":"function"}
]`

type teeMachine struct {
	TeeId      common.Address
	TeeProxyId common.Address
	Url        string
}

func runDoctor(ctx context.Context, rpcURL string, teeID common.Address, proxyURL string) error {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return fmt.Errorf("dialing rpc: %w", err)
	}
	defer client.Close()

	parsed, err := abi.JSON(strings.NewReader(doctorABI))
	if err != nil {
		return err
	}
	diamond := common.HexToAddress(LiveFlareTeeManager)

	call := func(method string, out *[]any, args ...any) error {
		data, err := parsed.Pack(method, args...)
		if err != nil {
			return err
		}
		raw, err := client.CallContract(ctx, ethereumCall(diamond, data), nil)
		if err != nil {
			return err
		}
		vals, err := parsed.Unpack(method, raw)
		if err != nil {
			return err
		}
		*out = vals
		return nil
	}

	fmt.Printf("TrustLine FCC doctor\n  diamond %s\n  teeId   %s\n\n", diamond.Hex(), teeID.Hex())

	// 1. Live deployment.
	//
	// Distinguish an RPC failure from a genuinely empty address. Conflating them reports
	// "wrong network?" on a transient blip, which is a confidently wrong diagnosis — the exact
	// failure mode this tool exists to prevent.
	code, err := client.CodeAt(ctx, diamond, nil)
	switch {
	case err != nil:
		warn(fmt.Sprintf("could not read code at the diamond (RPC error: %v)", err))
		fmt.Println("     This is an RPC problem, not necessarily a deployment problem. Retry before")
		fmt.Println("     concluding anything about your stack.")
		return nil
	case len(code) == 0:
		fail("FlareTeeManager has no code at the live address — wrong network?")
		return nil
	}
	pass(fmt.Sprintf("live FlareTeeManager has code (%d bytes)", len(code)))
	fmt.Printf("     if you see FunctionNotFound anywhere, check nothing still points at\n     %s (dead since 22 Jul 2026)\n", DeadFlareTeeManager)

	// 2 + 3. Registration and status.
	var out []any
	if err := call("getExtensionId", &out, teeID); err != nil {
		fail("getExtensionId reverted — this teeId is not registered (TeeNotFound).")
		fmt.Println("     After a redeploy, registrations may have been wiped: re-run pre-build for a")
		fmt.Println("     fresh EXTENSION_ID, then post-build, then register-tee -command rRap.")
		return nil
	}
	extID := out[0].(*big.Int)
	pass(fmt.Sprintf("registered to extension %s", extID))

	if err := call("getTeeMachineStatus", &out, teeID); err != nil {
		fail("getTeeMachineStatus reverted — teeId not found")
		return nil
	}
	status := out[0].(uint8)
	switch status {
	case StatusProduction:
		pass("status 2 (PRODUCTION)")
	case StatusInitialized:
		fail("status 1 (INITIALIZED) — will NOT receive dispatches")
		fmt.Println("     A simulated TEE reaches PRODUCTION in seconds on a current stack. Stuck here")
		fmt.Println("     almost always means the URL on-chain is dead, so the availability check")
		fmt.Println("     cannot reach you. Fix the URL, then re-run post-build.")
	default:
		fail(fmt.Sprintf("status %d — not PRODUCTION, will not receive dispatches", status))
	}

	// 4. Availability freshness.
	//
	// Reported as INFO, deliberately not as a failure. Measured on Coston2 2026-08-14: every one of
	// five sampled machines — including Flare's own tee-proxy-coston2-*.flare.rocks — had a lapsed
	// availability window, while 448 machines were still listed as active. So a lapsed window here
	// is NOT on its own evidence that your machine is broken, and treating it as the smoking gun
	// sends you down the wrong path. A fresh check is obtained via
	// requestAvailabilityCheckAttestation rather than maintained continuously.
	if err := call("getAvailabilityCheckValidity", &out, teeID); err != nil {
		warn("could not read availability check validity")
	} else {
		endTs := out[0].(uint64)
		left := time.Until(time.Unix(int64(endTs), 0))
		if left > 0 {
			pass(fmt.Sprintf("availability window valid for another %s", left.Truncate(time.Minute)))
		} else {
			info(fmt.Sprintf("availability window lapsed %s ago (validity duration is %s)",
				(-left).Truncate(time.Minute), availabilityFreshness))
			fmt.Println("     Not necessarily a fault: this is lapsed for most Coston2 machines,")
			fmt.Println("     including Flare's own. Only worth chasing if everything else is green.")
		}
	}

	// 5. Registered URL.
	if err := call("getTeeMachine", &out, teeID); err != nil {
		warn("could not read machine record")
		return nil
	}
	m := *abi.ConvertType(out[0], new(teeMachine)).(*teeMachine)
	fmt.Printf("\n  URL on-chain: %s\n", m.Url)

	if !strings.HasPrefix(m.Url, "https://") {
		fail("registered URL is not HTTPS — providers require a valid public HTTPS endpoint")
	}
	for _, bad := range []string{"trycloudflare.com", "ngrok-free.dev", "ngrok.io", "githubpreview", "app.github.dev"} {
		if strings.Contains(m.Url, bad) {
			warn(fmt.Sprintf("URL looks like an ephemeral tunnel (%s)", bad))
			fmt.Println("     These hostnames change on restart, but the URL is stored ON-CHAIN, so providers")
			fmt.Println("     keep POSTing to the dead one. Use a NAMED cloudflared tunnel or a reserved ngrok")
			fmt.Println("     domain. If it rotated: update EXT_PROXY_URL and re-run post-build.")
			break
		}
	}
	if proxyURL != "" {
		if strings.TrimRight(proxyURL, "/") != strings.TrimRight(m.Url, "/") {
			fail(fmt.Sprintf("MISMATCH: you passed -proxy %s but the chain says %s", proxyURL, m.Url))
			fmt.Println("     Providers push to the on-chain URL. If that is not what you are serving,")
			fmt.Println("     instructions will never arrive and /action/result will 404 forever.")
		} else {
			pass("-proxy matches the URL registered on-chain")
		}
	}

	// 6. Reachability.
	target := m.Url
	if target != "" {
		fmt.Println()
		checkURL(ctx, strings.TrimRight(target, "/")+"/info", "GET /info on the registered URL")
	}

	fmt.Println("\n  Not checkable from here: whether each individual provider attempted delivery and")
	fmt.Println("  what HTTP response it got. That lives in provider logs. If everything above is")
	fmt.Println("  green and the instruction still never lands, report the extension ID, teeId,")
	fmt.Println("  dispatch tx, registered URL, machine status and /action/status output.")
	return nil
}

func checkURL(ctx context.Context, url, label string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fail(label + ": " + err.Error())
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fail(fmt.Sprintf("%s: unreachable (%v)", label, err))
		fmt.Println("     Providers reach your proxy over the public internet at this exact hostname.")
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		pass(label + ": 200")
	} else {
		warn(fmt.Sprintf("%s: HTTP %d", label, resp.StatusCode))
	}
}

func pass(msg string) { fmt.Printf("  [ok]   %s\n", msg) }
func info(msg string) { fmt.Printf("  [info] %s\n", msg) }
func warn(msg string) { fmt.Printf("  [warn] %s\n", msg) }
func fail(msg string) { fmt.Printf("  [FAIL] %s\n", msg) }
