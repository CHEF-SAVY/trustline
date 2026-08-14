// Deployment addresses. Fill these in after running:
//   forge script script/Deploy.s.sol:DeployScript --rpc-url coston2 --broadcast
export const CONFIG = {
  chainId: 114,
  chainIdHex: "0x72",
  chainName: "Flare Testnet Coston2",
  rpcUrl: "https://coston2-api.flare.network/ext/C/rpc",
  explorer: "https://coston2-explorer.flare.network",
  nativeCurrency: { name: "Coston2 Flare", symbol: "C2FLR", decimals: 18 },

  // Filled in at deployment.
  //
  // These point at the DEMO stack (script/DeployDemo.s.sol), deployed because FCC's
  // MachineManager.toProduction() reverts with no revert data on Coston2, stranding our TEE
  // machine at status 1 (INITIALIZED) so the production verifier can never accept it.
  // The demo stack differs in exactly one way: UnderwritingVerifier reads machine status through
  // DemoTeeMachineRegistry (0x6209AcbaEa55ccCD874F58aA4B5eE889128bD75B), which reports a
  // registered machine as PRODUCTION. Signature verification, signer identity and extension
  // membership are all still checked for real, on-chain. See src/demo/DemoTeeMachineRegistry.sol.
  //
  // Original production stack, restore once toProduction works upstream:
  //   creditRegistry: "0xFC7aeDaCcD34AA685d60141BFFE9568FB71f8D9A"
  //   trustLinePool:  "0xE2A6be036cfFedf83406772D31e745cBA8EA3e58"
  creditRegistry: "0x539296b1A1210A7a6aEC99E2d311d0a89F350f69",
  trustLinePool: "0x41aC977D774cB86EC7f9b3125776C50e97bd9CE6",
  instructionSender: "0x33C7E0D2d9da4eF91de1C99Cfd33692e640DfD0E",

  // Pool asset. Coston2 FXRP, resolved at runtime via
  //   FlareContractRegistry -> AssetManagerFXRP -> fAsset()
  // Never hardcode this for mainnet; re-resolve per network.
  poolAsset: "0x0b6A3645c240605887a5532109323A3E12273dc7", // FTestXRP on Coston2
  poolAssetSymbol: "FTestXRP",

  // CRITICAL: FXRP has 6 decimals, NOT the usual 18. Getting this wrong scales every
  // amount by 10^12 - "borrow 1" would request a trillion units. Verified on-chain:
  //   cast call 0x0b6A...3dc7 "decimals()(uint8)" --rpc-url coston2  =>  6
  poolAssetDecimals: 6,

  // Must match TrustLinePool's constructor args.
  extensionId: 66285,

  standardLtvBips: 5000,
  liquidationThresholdBips: 8500,
};
