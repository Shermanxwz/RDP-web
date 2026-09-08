# Microsoft RDP client compatibility

## Android / iOS / macOS

Microsoft documents the legacy `rdp://query_string` URI scheme for these platforms. RDP Web uses only documented attributes:

- `full address`
- `username`
- `domain`
- `gatewayhostname`
- `gatewayusagemethod`
- `audiomode`

Passwords are not part of Microsoft's documented URI attribute set and RDP Web never attempts to put one in a URL.

## Windows

The documented `ms-rd:` URI scheme does not expose a generic `connect to arbitrary PC with these parameters` command. For normal PC connections RDP Web therefore generates a `.rdp` file and lets Windows open it with Remote Desktop Connection (`mstsc.exe`).

Starting with Windows security changes introduced in 2026, opening downloaded RDP files can show additional security warnings. This is a Windows security feature, not an RDP Web error. Enterprises that need a managed low-friction experience should follow Microsoft's RDP file signing and trusted-publisher policy guidance rather than disabling protections.

## Linux

Microsoft does not provide the same ordinary-PC client handoff path used above. RDP Web remains useful as a profile catalog and can export `.rdp` files, but the project does not claim a Microsoft-client one-click connection experience on Linux.
