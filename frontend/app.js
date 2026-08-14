// TrustLine frontend.
//
// Deliberately dependency-free: no bundler, no npm install, no CDN (a strict CSP would block one
// anyway). It talks to the chain over EIP-1193 + raw eth_call, which is enough for the five states
// the UI needs and keeps the whole app auditable in one file.
//
// The five states come straight from what the contracts can actually tell us:
//   not connected → not attested → pending → attested → expired

import { CONFIG } from "./config.js";

const $ = (id) => document.getElementById(id);

let account = null;
let pollTimer = null;
let countdownTimer = null;

// --- minimal ABI encoding (only what we need) --------------------------------

const enc = new TextEncoder();

// Precomputed 4-byte selectors, generated with `cast sig "<signature>"` against the deployed ABIs.
// Shipping these avoids bundling a keccak implementation just to derive them at runtime.
// Regenerate if a signature changes — a stale selector fails as an empty eth_call return, not a
// loud error, so `npm run check-selectors` (see README) asserts them against the ABI.
const SELECTORS = {
  hasValidAttestation: "0x6aae21db", // hasValidAttestation(address)
  getAttestation: "0xf9b71797", // getAttestation(address)
  currentMaxLTVBips: "0xc9e52a96", // currentMaxLTVBips(address)
  collateralOf: "0x1aefb107", // collateralOf(address)
  debtOf: "0xd283e75f", // debtOf(address)
  availableToBorrow: "0x2c38199e", // availableToBorrow(address)
  currentLTVBips: "0xfd1d1538", // currentLTVBips(address)
  requestCreditCheck: "0x24793e2a", // requestCreditCheck(string)
  deposit: "0xb6b55f25", // deposit(uint256)
  borrow: "0xc5ebeaec", // borrow(uint256)
  repay: "0x371fd8e6", // repay(uint256)
};

const pad = (hex) => hex.replace(/^0x/, "").padStart(64, "0");
const addrArg = (a) => pad(a.toLowerCase());

async function rpc(method, params) {
  if (!window.ethereum) throw new Error("No wallet found. Install MetaMask or a Flare-compatible wallet.");
  return window.ethereum.request({ method, params });
}

async function ethCall(to, data) {
  if (!to) throw new Error("contract address not configured");
  return rpc("eth_call", [{ to, data }, "latest"]);
}

const hexToBig = (h) => (h && h !== "0x" ? BigInt(h) : 0n);

function sliceWord(hex, i) {
  const body = hex.replace(/^0x/, "");
  return "0x" + body.slice(i * 64, (i + 1) * 64);
}

// --- formatting --------------------------------------------------------------

const fmtUnits = (v, dp = CONFIG.poolAssetDecimals) => {
  const s = v.toString().padStart(dp + 1, "0");
  const whole = s.slice(0, -dp) || "0";
  const frac = s.slice(-dp).replace(/0+$/, "").slice(0, 4);
  return frac ? `${whole}.${frac}` : whole;
};
const pct = (bips) => `${(Number(bips) / 100).toFixed(bips % 100 === 0 ? 0 : 2)}%`;
const shortAddr = (a) => `${a.slice(0, 6)}…${a.slice(-4)}`;

function fmtDuration(secs) {
  if (secs <= 0) return "expired";
  const h = Math.floor(secs / 3600);
  const m = Math.floor((secs % 3600) / 60);
  const s = secs % 60;
  if (h > 0) return `${h}h ${m}m left`;
  if (m > 0) return `${m}m ${s}s left`;
  return `${s}s left`;
}

// --- UI state ----------------------------------------------------------------

function setState(kind, text) {
  $("stateText").textContent = text;
  const dot = document.querySelector("#stateBadge .dot");
  dot.className = "dot" + (kind ? " " + kind : "");
}

function show(which) {
  for (const id of ["attested", "notAttested", "pending"]) {
    $(id).classList.toggle("hide", id !== which);
  }
}

function showErr(id, msg) {
  const el = $(id);
  if (!msg) return el.classList.add("hide");
  el.textContent = msg;
  el.classList.remove("hide");
}

// --- chain reads -------------------------------------------------------------

async function readCredit() {
  const reg = CONFIG.creditRegistry;
  if (!reg) return null;

  const valid = hexToBig(await ethCall(reg, SELECTORS.hasValidAttestation + addrArg(account))) === 1n;
  const raw = await ethCall(reg, SELECTORS.getAttestation + addrArg(account));

  // StoredAttestation: riskTier, maxLTVBips, issuedAt, expiry, xrplAddressHash, teeId, instructionId
  const att = {
    valid,
    riskTier: Number(hexToBig(sliceWord(raw, 0))),
    maxLTVBips: Number(hexToBig(sliceWord(raw, 1))),
    issuedAt: Number(hexToBig(sliceWord(raw, 2))),
    expiry: Number(hexToBig(sliceWord(raw, 3))),
    xrplAddressHash: sliceWord(raw, 4),
    teeId: "0x" + sliceWord(raw, 5).slice(-40),
  };
  return att;
}

async function readPool() {
  const pool = CONFIG.trustLinePool;
  if (!pool) return null;
  const [col, debt, avail, ltv] = await Promise.all([
    ethCall(pool, SELECTORS.collateralOf + addrArg(account)),
    ethCall(pool, SELECTORS.debtOf + addrArg(account)),
    ethCall(pool, SELECTORS.availableToBorrow + addrArg(account)),
    ethCall(pool, SELECTORS.currentLTVBips + addrArg(account)),
  ]);
  return {
    collateral: hexToBig(col),
    debt: hexToBig(debt),
    available: hexToBig(avail),
    ltvBips: hexToBig(ltv),
  };
}

// --- render ------------------------------------------------------------------

function renderComparison(collateral, attestedBips) {
  const std = (collateral * BigInt(CONFIG.standardLtvBips)) / 10000n;
  const uw = (collateral * BigInt(attestedBips)) / 10000n;

  $("stdAmt").textContent = fmtUnits(std);
  $("uwAmt").textContent = fmtUnits(uw);
  $("stdPct").textContent = pct(CONFIG.standardLtvBips);
  $("uwPct").textContent = pct(attestedBips);

  const max = Number(attestedBips) || 1;
  $("stdBar").style.width = `${(CONFIG.standardLtvBips / max) * 100}%`;
  $("uwBar").style.width = "100%";
}

function startCountdown(expiry) {
  clearInterval(countdownTimer);
  const tick = () => {
    const left = expiry - Math.floor(Date.now() / 1000);
    $("expiryCountdown").textContent = fmtDuration(left);
    if (left <= 0) {
      clearInterval(countdownTimer);
      refresh();
    }
  };
  tick();
  countdownTimer = setInterval(tick, 1000);
}

async function refresh() {
  if (!account) return;
  showErr("reqErr", null);

  try {
    const att = await readCredit();
    const pool = await readPool();

    if (pool) {
      $("colV").textContent = fmtUnits(pool.collateral);
      $("debtV").textContent = fmtUnits(pool.debt);
      $("availV").textContent = fmtUnits(pool.available);
      $("curLtvV").textContent = pool.debt > 0n ? pct(pool.ltvBips) : "—";
      $("liqV").textContent = pct(CONFIG.liquidationThresholdBips);
    }

    if (!att || att.issuedAt === 0) {
      setState("", "Not attested");
      $("expiryCountdown").textContent = "";
      clearInterval(countdownTimer);
      show("notAttested");
      return;
    }

    if (!att.valid) {
      // Has an attestation on record, but it has lapsed.
      setState("bad", "Attestation expired");
      $("expiryCountdown").textContent = "expired";
      clearInterval(countdownTimer);
      show("notAttested");
      $("requestBtn").textContent = "Re-attest";
      return;
    }

    setState("good", `Attested · tier ${att.riskTier}`);
    $("tierV").textContent = att.riskTier;
    $("ltvV").textContent = pct(att.maxLTVBips);
    $("teeV").textContent = shortAddr(att.teeId);
    renderComparison(pool ? pool.collateral : 0n, att.maxLTVBips);
    startCountdown(att.expiry);
    show("attested");
    stopPolling();
  } catch (e) {
    showErr("reqErr", e.message || String(e));
  }
}

// --- polling while pending ---------------------------------------------------

function startPolling() {
  stopPolling();
  // The relay leg has no callback, so polling is the honest mechanism. 8s is frequent enough to
  // feel responsive without hammering the RPC.
  pollTimer = setInterval(refresh, 8000);
}
function stopPolling() {
  clearInterval(pollTimer);
  pollTimer = null;
}

// --- actions -----------------------------------------------------------------

async function connect() {
  try {
    showErr("netErr", null);
    const accounts = await rpc("eth_requestAccounts", []);
    account = accounts[0];
    $("account").textContent = shortAddr(account);

    const chainId = await rpc("eth_chainId", []);
    if (parseInt(chainId, 16) !== CONFIG.chainId) {
      try {
        await rpc("wallet_switchEthereumChain", [{ chainId: CONFIG.chainIdHex }]);
      } catch {
        await rpc("wallet_addEthereumChain", [{
          chainId: CONFIG.chainIdHex,
          chainName: CONFIG.chainName,
          rpcUrls: [CONFIG.rpcUrl],
          nativeCurrency: CONFIG.nativeCurrency,
          blockExplorerUrls: [CONFIG.explorer],
        }]);
      }
    }
    await refresh();
  } catch (e) {
    showErr("netErr", e.message || String(e));
  }
}

async function requestCreditCheck() {
  const xrpl = $("xrplAddr").value.trim();
  if (!xrpl) return showErr("reqErr", "Enter an XRPL address.");
  if (!CONFIG.instructionSender) return showErr("reqErr", "instructionSender not configured — deploy first.");

  try {
    showErr("reqErr", null);
    $("requestBtn").disabled = true;

    // requestCreditCheck(string) — dynamic string: head offset, length, padded bytes.
    const sel = SELECTORS.requestCreditCheck;
    const bytes = enc.encode(xrpl);
    const len = pad(bytes.length.toString(16));
    let body = "";
    for (const b of bytes) body += b.toString(16).padStart(2, "0");
    body = body.padEnd(Math.ceil(bytes.length / 32) * 64, "0");
    const data = sel + pad("20") + len + body;

    const tx = await rpc("eth_sendTransaction", [{
      from: account, to: CONFIG.instructionSender, data,
    }]);

    $("pendingTx").textContent = tx;
    setState("pending", "Credit check pending");
    show("pending");
    startPolling();
  } catch (e) {
    showErr("reqErr", e.message || String(e));
  } finally {
    $("requestBtn").disabled = false;
  }
}

async function poolAction(kind) {
  const raw = $("amount").value.trim();
  if (!raw) return showErr("poolErr", "Enter an amount.");
  if (!CONFIG.trustLinePool) return showErr("poolErr", "trustLinePool not configured — deploy first.");

  const selectors = { deposit: SELECTORS.deposit, borrow: SELECTORS.borrow, repay: SELECTORS.repay };
  try {
    showErr("poolErr", null);
    // Scale by the ASSET's decimals, not a hardcoded 18. FXRP on Coston2 uses 6.
    const amount = BigInt(Math.round(parseFloat(raw) * 10 ** CONFIG.poolAssetDecimals));
    const data = selectors[kind] + pad(amount.toString(16));
    await rpc("eth_sendTransaction", [{ from: account, to: CONFIG.trustLinePool, data }]);
    setTimeout(refresh, 3000);
  } catch (e) {
    showErr("poolErr", e.message || String(e));
  }
}

// --- wiring ------------------------------------------------------------------

$("connect").onclick = connect;
$("requestBtn").onclick = requestCreditCheck;
$("refreshBtn").onclick = refresh;
$("depositBtn").onclick = () => poolAction("deposit");
$("borrowBtn").onclick = () => poolAction("borrow");
$("repayBtn").onclick = () => poolAction("repay");

if (window.ethereum) {
  window.ethereum.on?.("accountsChanged", (a) => {
    account = a[0] || null;
    $("account").textContent = account ? shortAddr(account) : "not connected";
    if (account) refresh();
  });
  window.ethereum.on?.("chainChanged", () => window.location.reload());
}

setState("", "Not connected");
show("notAttested");
