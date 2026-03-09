# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly:

1. **Do NOT** open a public GitHub issue
2. Email security concerns to: **security@zion.example** (replace with actual contact)
3. Include a description of the vulnerability, steps to reproduce, and potential impact

We will acknowledge your report within 48 hours and provide a timeline for a fix.

## Scope

The following are in scope for security reports:

- **Command signature bypass** — Any way to execute unsigned or forged hub commands on a node
- **Wallet key exposure** — Unintended disclosure of private keys
- **Container escape** — Breaking out of agent containers to access the host
- **Authentication bypass** — Circumventing wallet-based authentication or JWT validation
- **Denial of service** — Crashing the node daemon via crafted input

## Known Limitations

- **Wallet keystore** stores private keys as unencrypted hex in `~/.zion-node/wallet.json` with file permissions `0600`. This is a known design trade-off for simplicity. Users should protect this file appropriately and consider full-disk encryption.

## Supported Versions

| Version | Supported |
|---------|-----------|
| latest  | ✅        |

## Disclosure Policy

- We follow [coordinated disclosure](https://en.wikipedia.org/wiki/Coordinated_vulnerability_disclosure)
- Security fixes will be released as patch versions
- Credits will be given to reporters (unless anonymity is requested)
