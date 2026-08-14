// Generated from contracts/ via `forge inspect <c> abi`. Do not hand-edit.
export const ABI = {
  "CreditRegistry": [
    {
      "type": "constructor",
      "inputs": [
        {
          "name": "_verifier",
          "type": "address",
          "internalType": "contract UnderwritingVerifier"
        }
      ],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "VERIFIER",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "address",
          "internalType": "contract UnderwritingVerifier"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "consumedInstructionIds",
      "inputs": [
        {
          "name": "",
          "type": "bytes32",
          "internalType": "bytes32"
        }
      ],
      "outputs": [
        {
          "name": "",
          "type": "bool",
          "internalType": "bool"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "currentMaxLTVBips",
      "inputs": [
        {
          "name": "_borrower",
          "type": "address",
          "internalType": "address"
        }
      ],
      "outputs": [
        {
          "name": "",
          "type": "uint16",
          "internalType": "uint16"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "currentRiskTier",
      "inputs": [
        {
          "name": "_borrower",
          "type": "address",
          "internalType": "address"
        }
      ],
      "outputs": [
        {
          "name": "",
          "type": "uint8",
          "internalType": "uint8"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "getAttestation",
      "inputs": [
        {
          "name": "_borrower",
          "type": "address",
          "internalType": "address"
        }
      ],
      "outputs": [
        {
          "name": "",
          "type": "tuple",
          "internalType": "struct CreditRegistry.StoredAttestation",
          "components": [
            {
              "name": "riskTier",
              "type": "uint8",
              "internalType": "uint8"
            },
            {
              "name": "maxLTVBips",
              "type": "uint16",
              "internalType": "uint16"
            },
            {
              "name": "issuedAt",
              "type": "uint64",
              "internalType": "uint64"
            },
            {
              "name": "expiry",
              "type": "uint64",
              "internalType": "uint64"
            },
            {
              "name": "xrplAddressHash",
              "type": "bytes32",
              "internalType": "bytes32"
            },
            {
              "name": "teeId",
              "type": "address",
              "internalType": "address"
            },
            {
              "name": "instructionId",
              "type": "bytes32",
              "internalType": "bytes32"
            }
          ]
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "hasValidAttestation",
      "inputs": [
        {
          "name": "_borrower",
          "type": "address",
          "internalType": "address"
        }
      ],
      "outputs": [
        {
          "name": "",
          "type": "bool",
          "internalType": "bool"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "submitAttestation",
      "inputs": [
        {
          "name": "_data",
          "type": "bytes",
          "internalType": "bytes"
        },
        {
          "name": "_id",
          "type": "bytes32",
          "internalType": "bytes32"
        },
        {
          "name": "_status",
          "type": "uint8",
          "internalType": "uint8"
        },
        {
          "name": "_signature",
          "type": "bytes",
          "internalType": "bytes"
        }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    },
    {
      "type": "event",
      "name": "CreditAttested",
      "inputs": [
        {
          "name": "borrower",
          "type": "address",
          "indexed": true,
          "internalType": "address"
        },
        {
          "name": "riskTier",
          "type": "uint8",
          "indexed": false,
          "internalType": "uint8"
        },
        {
          "name": "maxLTV",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        },
        {
          "name": "expiry",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        },
        {
          "name": "teeId",
          "type": "address",
          "indexed": true,
          "internalType": "address"
        },
        {
          "name": "instructionId",
          "type": "bytes32",
          "indexed": true,
          "internalType": "bytes32"
        }
      ],
      "anonymous": false
    },
    {
      "type": "error",
      "name": "AttestationExpired",
      "inputs": [
        {
          "name": "expiry",
          "type": "uint64",
          "internalType": "uint64"
        },
        {
          "name": "nowTs",
          "type": "uint256",
          "internalType": "uint256"
        }
      ]
    },
    {
      "type": "error",
      "name": "AttestationNotNewer",
      "inputs": [
        {
          "name": "storedIssuedAt",
          "type": "uint64",
          "internalType": "uint64"
        },
        {
          "name": "incomingIssuedAt",
          "type": "uint64",
          "internalType": "uint64"
        }
      ]
    },
    {
      "type": "error",
      "name": "AttestationReplayed",
      "inputs": [
        {
          "name": "instructionId",
          "type": "bytes32",
          "internalType": "bytes32"
        }
      ]
    }
  ],
  "TrustLinePool": [
    {
      "type": "constructor",
      "inputs": [
        {
          "name": "_asset",
          "type": "address",
          "internalType": "contract IERC20"
        },
        {
          "name": "_creditRegistry",
          "type": "address",
          "internalType": "contract CreditRegistry"
        },
        {
          "name": "_standardLtvBips",
          "type": "uint16",
          "internalType": "uint16"
        },
        {
          "name": "_liquidationThresholdBips",
          "type": "uint16",
          "internalType": "uint16"
        }
      ],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "ASSET",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "address",
          "internalType": "contract IERC20"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "BIPS",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "uint16",
          "internalType": "uint16"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "CREDIT_REGISTRY",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "address",
          "internalType": "contract CreditRegistry"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "LIQUIDATION_THRESHOLD_BIPS",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "uint16",
          "internalType": "uint16"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "STANDARD_LTV_BIPS",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "uint16",
          "internalType": "uint16"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "availableLiquidity",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "availableToBorrow",
      "inputs": [
        {
          "name": "_user",
          "type": "address",
          "internalType": "address"
        }
      ],
      "outputs": [
        {
          "name": "",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "borrow",
      "inputs": [
        {
          "name": "_amount",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "collateralOf",
      "inputs": [
        {
          "name": "",
          "type": "address",
          "internalType": "address"
        }
      ],
      "outputs": [
        {
          "name": "",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "currentLTVBips",
      "inputs": [
        {
          "name": "_user",
          "type": "address",
          "internalType": "address"
        }
      ],
      "outputs": [
        {
          "name": "",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "debtOf",
      "inputs": [
        {
          "name": "",
          "type": "address",
          "internalType": "address"
        }
      ],
      "outputs": [
        {
          "name": "",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "deposit",
      "inputs": [
        {
          "name": "_amount",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "effectiveLTVBips",
      "inputs": [
        {
          "name": "_user",
          "type": "address",
          "internalType": "address"
        }
      ],
      "outputs": [
        {
          "name": "ltvBips",
          "type": "uint16",
          "internalType": "uint16"
        },
        {
          "name": "underwritten",
          "type": "bool",
          "internalType": "bool"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "isLiquidatable",
      "inputs": [
        {
          "name": "_user",
          "type": "address",
          "internalType": "address"
        }
      ],
      "outputs": [
        {
          "name": "",
          "type": "bool",
          "internalType": "bool"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "liquidate",
      "inputs": [
        {
          "name": "_user",
          "type": "address",
          "internalType": "address"
        }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "maxDebtFor",
      "inputs": [
        {
          "name": "_user",
          "type": "address",
          "internalType": "address"
        }
      ],
      "outputs": [
        {
          "name": "",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "repay",
      "inputs": [
        {
          "name": "_amount",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "totalCollateral",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "totalDebt",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "withdraw",
      "inputs": [
        {
          "name": "_amount",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "outputs": [],
      "stateMutability": "nonpayable"
    },
    {
      "type": "event",
      "name": "Borrowed",
      "inputs": [
        {
          "name": "user",
          "type": "address",
          "indexed": true,
          "internalType": "address"
        },
        {
          "name": "amount",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        },
        {
          "name": "appliedLTVBips",
          "type": "uint16",
          "indexed": false,
          "internalType": "uint16"
        },
        {
          "name": "underwritten",
          "type": "bool",
          "indexed": false,
          "internalType": "bool"
        }
      ],
      "anonymous": false
    },
    {
      "type": "event",
      "name": "Deposited",
      "inputs": [
        {
          "name": "user",
          "type": "address",
          "indexed": true,
          "internalType": "address"
        },
        {
          "name": "amount",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        }
      ],
      "anonymous": false
    },
    {
      "type": "event",
      "name": "Liquidated",
      "inputs": [
        {
          "name": "user",
          "type": "address",
          "indexed": true,
          "internalType": "address"
        },
        {
          "name": "liquidator",
          "type": "address",
          "indexed": true,
          "internalType": "address"
        },
        {
          "name": "debtRepaid",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        },
        {
          "name": "collateralSeized",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        }
      ],
      "anonymous": false
    },
    {
      "type": "event",
      "name": "Repaid",
      "inputs": [
        {
          "name": "user",
          "type": "address",
          "indexed": true,
          "internalType": "address"
        },
        {
          "name": "amount",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        }
      ],
      "anonymous": false
    },
    {
      "type": "event",
      "name": "Withdrawn",
      "inputs": [
        {
          "name": "user",
          "type": "address",
          "indexed": true,
          "internalType": "address"
        },
        {
          "name": "amount",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        }
      ],
      "anonymous": false
    },
    {
      "type": "error",
      "name": "ExceedsBorrowingPower",
      "inputs": [
        {
          "name": "requested",
          "type": "uint256",
          "internalType": "uint256"
        },
        {
          "name": "available",
          "type": "uint256",
          "internalType": "uint256"
        }
      ]
    },
    {
      "type": "error",
      "name": "InsufficientCollateral",
      "inputs": []
    },
    {
      "type": "error",
      "name": "InsufficientLiquidity",
      "inputs": [
        {
          "name": "requested",
          "type": "uint256",
          "internalType": "uint256"
        },
        {
          "name": "available",
          "type": "uint256",
          "internalType": "uint256"
        }
      ]
    },
    {
      "type": "error",
      "name": "NoDebt",
      "inputs": []
    },
    {
      "type": "error",
      "name": "PositionHealthy",
      "inputs": [
        {
          "name": "ltvBips",
          "type": "uint256",
          "internalType": "uint256"
        },
        {
          "name": "thresholdBips",
          "type": "uint16",
          "internalType": "uint16"
        }
      ]
    },
    {
      "type": "error",
      "name": "ReentrancyGuardReentrantCall",
      "inputs": []
    },
    {
      "type": "error",
      "name": "RepayExceedsDebt",
      "inputs": [
        {
          "name": "amount",
          "type": "uint256",
          "internalType": "uint256"
        },
        {
          "name": "debt",
          "type": "uint256",
          "internalType": "uint256"
        }
      ]
    },
    {
      "type": "error",
      "name": "SafeERC20FailedOperation",
      "inputs": [
        {
          "name": "token",
          "type": "address",
          "internalType": "address"
        }
      ]
    },
    {
      "type": "error",
      "name": "ZeroAmount",
      "inputs": []
    }
  ],
  "TrustLineInstructionSender": [
    {
      "type": "constructor",
      "inputs": [
        {
          "name": "_teeExtensionRegistry",
          "type": "address",
          "internalType": "contract ITeeExtensionRegistry"
        },
        {
          "name": "_teeMachineRegistry",
          "type": "address",
          "internalType": "contract ITeeMachineRegistry"
        }
      ],
      "stateMutability": "nonpayable"
    },
    {
      "type": "function",
      "name": "OP_COMMAND_SCORE_BORROWER",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "bytes32",
          "internalType": "bytes32"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "OP_TYPE_TRUSTLINE",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "bytes32",
          "internalType": "bytes32"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "TEE_EXTENSION_REGISTRY",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "address",
          "internalType": "contract ITeeExtensionRegistry"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "TEE_MACHINE_COUNT",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "TEE_MACHINE_REGISTRY",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "address",
          "internalType": "contract ITeeMachineRegistry"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "extensionId",
      "inputs": [],
      "outputs": [
        {
          "name": "",
          "type": "uint256",
          "internalType": "uint256"
        }
      ],
      "stateMutability": "view"
    },
    {
      "type": "function",
      "name": "requestCreditCheck",
      "inputs": [
        {
          "name": "_xrplAddress",
          "type": "string",
          "internalType": "string"
        }
      ],
      "outputs": [
        {
          "name": "instructionId",
          "type": "bytes32",
          "internalType": "bytes32"
        }
      ],
      "stateMutability": "payable"
    },
    {
      "type": "function",
      "name": "setExtensionId",
      "inputs": [],
      "outputs": [],
      "stateMutability": "nonpayable"
    },
    {
      "type": "event",
      "name": "CreditCheckRequested",
      "inputs": [
        {
          "name": "borrower",
          "type": "address",
          "indexed": true,
          "internalType": "address"
        },
        {
          "name": "instructionId",
          "type": "bytes32",
          "indexed": true,
          "internalType": "bytes32"
        },
        {
          "name": "xrplAddress",
          "type": "string",
          "indexed": false,
          "internalType": "string"
        },
        {
          "name": "fee",
          "type": "uint256",
          "indexed": false,
          "internalType": "uint256"
        }
      ],
      "anonymous": false
    },
    {
      "type": "error",
      "name": "EmptyXrplAddress",
      "inputs": []
    },
    {
      "type": "error",
      "name": "ExtensionIdNotSet",
      "inputs": []
    },
    {
      "type": "error",
      "name": "NoTeeMachinesAvailable",
      "inputs": []
    }
  ]
};
